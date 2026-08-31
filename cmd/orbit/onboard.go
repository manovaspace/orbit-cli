package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/doctor/healer"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

// OnboardProgressEvent represents a structured progress notification emitted during onboarding.
type OnboardProgressEvent struct {
	Stage     session.Stage `json:"stage"`
	Status    string        `json:"status"` // "started", "in_progress", "completed", "failed", "skipped"
	Message   string        `json:"message"`
	Details   any           `json:"details,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

type progressEmitter struct {
	jsonMode bool
	out      io.Writer
}

func (e *progressEmitter) Emit(stage session.Stage, status, message string, details any) {
	if e.jsonMode {
		evt := OnboardProgressEvent{
			Stage:     stage,
			Status:    status,
			Message:   message,
			Details:   details,
			Timestamp: time.Now().UTC(),
		}
		data, _ := json.Marshal(evt)
		fmt.Fprintln(e.out, string(data))
	}
}

type onboardOptions struct {
	token                     string
	name                      string
	email                     string
	uid                       string
	edgeURL                   string
	workspace                 string
	manifest                  string
	sessionFile               string
	sshDir                    string
	diagBundle                string
	dryRun                    bool
	resume                    bool
	ignoreAndRemoveCheckpoint bool
	reset                     bool
	rollback                  bool
	nonInteractive            bool
	autoFix                   bool
	json                      bool
	skipStack                 bool
	startStack                bool
}

var stageOrder = map[session.Stage]int{
	session.StageInit:              0,
	session.StageDoctorPassed:      1,
	session.StageKeypairReady:      2,
	session.StageTokenClaimed:      3,
	session.StageNetworkConfigured: 4,
	session.StageReposCloned:       5,
	session.StageMCPConfigured:     6,
	session.StageDevStackReady:     7,
	session.StageCompleted:         8,
}

func isStageCompleted(current, target session.Stage) bool {
	return stageOrder[current] >= stageOrder[target]
}

func newOnboardCmd() *cobra.Command {
	opts := &onboardOptions{}

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive onboarding wizard with resume, claims, and stack provisioning",
		Long: `Interactive onboarding wizard that sets up developer identity, SSH keys,
repository clones, Cursor MCP integrations, and local development infrastructure.

Pro capabilities:
  --resume                          Resume an interrupted onboarding session
  --ignore-and-remove-checkpoint    Discard saved incomplete session checkpoint and start fresh
  --rollback                        Revert cloned repositories and provisioned credentials
  --diag-bundle                     Generate a sanitized diagnostic bundle for troubleshooting
  --auto-fix                        Automatically remediate missing prerequisites and toolchains
  --dry-run                         Run pre-flight diagnostics and preview onboarding actions`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnboard(cmd, args, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.token, "token", "t", "", "Cryptographically signed onboarding invite token")
	cmd.Flags().StringVarP(&opts.name, "name", "n", "", "Developer display name")
	cmd.Flags().StringVar(&opts.email, "email", "", "Developer email address")
	cmd.Flags().StringVarP(&opts.uid, "uid", "u", "", "Desired username / UID")
	cmd.Flags().StringVar(&opts.edgeURL, "edge-url", "", "Onboarding edge gateway URL (default: $ORBIT_SERVER or http://localhost:8080)")
	cmd.Flags().StringVarP(&opts.edgeURL, "server", "s", "", "Orbit server URL (alias for --edge-url)")
	cmd.Flags().StringVar(&opts.workspace, "workspace", "", "Target workspace root directory")
	cmd.Flags().StringVar(&opts.manifest, "manifest", "", "Path to workspace.yaml manifest")
	cmd.Flags().StringVar(&opts.sessionFile, "session-file", "", "Custom path to session persistence file")
	cmd.Flags().StringVar(&opts.sshDir, "ssh-dir", "", "Custom SSH directory path (default: ~/.ssh)")
	cmd.Flags().StringVar(&opts.diagBundle, "diag-bundle", "", "Generate a sanitized diagnostic tar.gz bundle (optional filename)")
	cmd.Flags().Lookup("diag-bundle").NoOptDefVal = "default"

	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Run pre-flight check and preview onboarding actions without making changes")
	cmd.Flags().BoolVar(&opts.resume, "resume", false, "Resume interrupted onboarding session from saved checkpoint")
	cmd.Flags().BoolVar(&opts.ignoreAndRemoveCheckpoint, "ignore-and-remove-checkpoint", false, "Discard saved incomplete session checkpoint and start fresh")
	cmd.Flags().BoolVar(&opts.reset, "reset", false, "Alias for --ignore-and-remove-checkpoint")
	cmd.Flags().Lookup("reset").Hidden = true
	cmd.Flags().BoolVar(&opts.rollback, "rollback", false, "Rollback provisioned resources and clear session")
	cmd.Flags().BoolVarP(&opts.autoFix, "auto-fix", "f", false, "Automatically install missing prerequisites and toolchain dependencies")
	cmd.Flags().BoolVar(&opts.autoFix, "fix", false, "Alias for --auto-fix")
	cmd.Flags().Lookup("fix").Hidden = true
	cmd.Flags().BoolVarP(&opts.nonInteractive, "yes", "y", false, "Skip interactive confirmation prompts and automatically proceed")
	cmd.Flags().BoolVar(&opts.nonInteractive, "non-interactive", false, "Run in non-interactive mode without prompting")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output JSON progress event stream")
	cmd.Flags().BoolVar(&opts.skipStack, "skip-stack", false, "Skip local dev stack initialization")
	cmd.Flags().BoolVar(&opts.startStack, "start-stack", false, "Automatically start local dev stack without prompting")

	return cmd
}

func runOnboard(cmd *cobra.Command, args []string, opts *onboardOptions) error {
	out := cmd.OutOrStdout()
	in := cmd.InOrStdin()
	emitter := &progressEmitter{jsonMode: opts.json, out: out}

	// 1. Resolve Session Manager
	sm, err := session.NewSessionManager(opts.sessionFile)
	if err != nil {
		return fmt.Errorf("failed to initialize session manager: %w", err)
	}

	workspaceRoot := findWorkspaceRoot(opts.workspace)

	// 2. Handle Reset / Ignore Flag
	if opts.ignoreAndRemoveCheckpoint || opts.reset {
		emitter.Emit(session.StageInit, "completed", "Clearing session state", nil)
		return executeReset(sm, out)
	}

	// 3. Handle Rollback Flag
	if opts.rollback {
		emitter.Emit(session.StageInit, "completed", "Rolling back onboarding state", nil)
		return executeRollback(cmd.Context(), workspaceRoot, opts.edgeURL, sm, out)
	}

	// 4. Handle Diagnostic Bundle Flag
	if opts.diagBundle != "" {
		targetBundle := opts.diagBundle
		if (targetBundle == "default" || targetBundle == "") && len(args) > 0 && args[0] != "" {
			targetBundle = args[0]
		}
		emitter.Emit(session.StageInit, "started", "Generating diagnostic bundle", nil)
		bundlePath, err := generateDiagnosticBundle(workspaceRoot, sm, targetBundle)
		if err != nil {
			emitter.Emit(session.StageInit, "failed", err.Error(), nil)
			return err
		}
		emitter.Emit(session.StageInit, "completed", fmt.Sprintf("Bundle generated: %s", bundlePath), map[string]string{"bundle_path": bundlePath})
		if !opts.json {
			fmt.Fprintln(out, titleStyle.Render("Orbit Diagnostic Bundle Generated"))
			fmt.Fprintf(out, "  %s  Sanitized diagnostic archive created at:\n     %s\n\n",
				iconOK,
				codeStyle.Render(bundlePath),
			)
			fmt.Fprintf(out, "  %s  Share this bundle with the platform team for triage.\n", iconInfo)
		}
		return nil
	}

	// 5. Launch interactive TUI wizard if running in an interactive terminal
	// and neither --non-interactive nor --json nor --dry-run is set.
	if istty.IsInteractiveSession() && !opts.nonInteractive && !opts.json && !opts.dryRun {
		return tuiOnboard.RunWizard(tuiOnboard.WizardOptions{
			SessionManager: sm,
			PreSetToken:    opts.token,
			Resume:         opts.resume,
			Reset:          opts.ignoreAndRemoveCheckpoint || opts.reset,
			Rollback:       opts.rollback,
		})
	}

	// 6. Render Banner (headless / non-interactive path)
	if !opts.json {
		banner := `
╔══════════════════════════════════════════════════════════════╗
║                  ORBIT DEVELOPER WIZARD                      ║
║         Zero-Leak Production Onboarding & Dev Stack          ║
╚══════════════════════════════════════════════════════════════╝`
		fmt.Fprintln(out, titleStyle.Render(banner))
	}

	// 6. Session Resume / Inception Check
	var s *session.SessionState
	hasPending := sm.HasPendingSession()

	if hasPending {
		loaded, loadErr := sm.LoadSession()
		if loadErr == nil && loaded != nil {
			if opts.resume {
				s = loaded
				if !opts.json {
					fmt.Fprintf(out, "  %s  Resuming existing session %s from stage: %s\n\n",
						iconOK,
						codeStyle.Render(s.ID),
						infoStyle.Render(string(s.CurrentStage)),
					)
				}
				emitter.Emit(s.CurrentStage, "resumed", fmt.Sprintf("Resuming session %s from stage %s", s.ID, s.CurrentStage), s)
			} else if !opts.nonInteractive && !opts.json {
				fmt.Fprintf(out, "  %s  Found incomplete onboarding session (%s, stage: %s).\n",
					iconInfo,
					codeStyle.Render(loaded.ID),
					warningStyle.Render(string(loaded.CurrentStage)),
				)
				if promptYesNo(in, out, "Would you like to resume this session? (Enter 'n' to discard checkpoint)", true) {
					s = loaded
					opts.resume = true
					emitter.Emit(s.CurrentStage, "resumed", fmt.Sprintf("Resuming session %s", s.ID), s)
				} else {
					_ = sm.ClearSession()
					s = sm.CreateSession(opts.email, opts.name)
					_ = sm.SaveSession(s)
					fmt.Fprintf(out, "  %s  Session checkpoint discarded. To automate starting fresh next time, use:\n     %s\n\n",
						iconOK,
						codeStyle.Render("orbit onboard --ignore-and-remove-checkpoint"),
					)
					emitter.Emit(session.StageInit, "started", "Starting fresh onboarding session", s)
				}
			} else {
				// In non-interactive mode without explicit flags
				if !opts.dryRun && opts.token != "" && !opts.resume && !opts.ignoreAndRemoveCheckpoint && !opts.reset {
					return fmt.Errorf("ongoing onboarding session (%s) detected; pass '--resume' to continue or '--ignore-and-remove-checkpoint' to discard checkpoint", loaded.ID)
				}
				_ = sm.ClearSession()
				s = sm.CreateSession(opts.email, opts.name)
				_ = sm.SaveSession(s)
			}
		}
	}

	if s == nil {
		s = sm.CreateSession(opts.email, opts.name)
		_ = sm.SaveSession(s)
		emitter.Emit(session.StageInit, "started", "Initialized onboarding session", s)
	}

	// ── STATE 1: Pre-flight Doctor Diagnostics ─────────────────────────────────
	if !isStageCompleted(s.CurrentStage, session.StageDoctorPassed) {
		emitter.Emit(session.StageInit, "in_progress", "Running pre-flight diagnostics", nil)
		if !opts.json {
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Step 1: Pre-Flight System Diagnostics ──────────────────"))
		}

		report := doctor.RunDiagnostics()
		passed, warnings, errorsCount := countDiagnosticResults(report)
		fixes := collectFixSuggestions(report)

		if !opts.json {
			renderDiagnosticResults(out, report, fixes, passed, warnings, errorsCount)
		}

		// Handle Dry Run Mode
		if opts.dryRun {
			emitter.Emit(session.StageInit, "completed", "Pre-flight dry run completed", map[string]int{"passed": passed, "errors": errorsCount})
			if !opts.json {
				preview := `
Actions that will be executed during onboarding:
  1. SSH Keypair: Detect or generate ed25519 key (~/.ssh/id_ed25519) and add to agent
  2. Identity Claim: Claim cryptographic invite token against edge provisioner
  3. WireGuard VPN: Configure local VPN profile for access to dev services
  4. Repositories: Clone core workspace repositories from workspace.yaml
  5. Cursor IDE & MCP: Configure .cursor/mcp.env and link workspace agent rules
  6. Dev Stack: Optionally start local Docker containers (orbit dev up)
`
				fmt.Fprintln(out, renderCard("DRY-RUN PREVIEW", preview))
			}
			return nil
		}

		// Check for auto-healable issues if there are diagnostic errors/healables
		reg := healer.NewDefaultRegistry()
		healableHealers := reg.FindHealers(report.Results)

		if len(healableHealers) > 0 {
			shouldHeal := opts.autoFix
			isNonInteractive := opts.nonInteractive

			if !shouldHeal {
				if isNonInteractive {
					// In non-interactive mode with -y / --yes / --non-interactive: execute healers automatically.
					shouldHeal = true
				} else if istty.IsInteractiveSession() || cmd.InOrStdin() != os.Stdin {
					if !opts.json {
						fmt.Fprintln(out)
					}
					shouldHeal = promptYesNo(in, out, "? Automatically install missing prerequisites to continue?", true)
				}
			}

			if shouldHeal {
				emitter.Emit(session.StageInit, "in_progress", "Auto-healing missing prerequisites", map[string]int{"healable_count": len(healableHealers)})
				if !opts.json {
					fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Auto-Healing Toolchains & Dependencies ─────────────────"))
				}

				_, _ = healer.RunHealers(cmd.Context(), report.Results, func(name, status string) {
					if !opts.json {
						if status == "Completed successfully" {
							fmt.Fprintf(out, "  %s  %-24s %s\n", iconOK, boldStyle.Render(name), successStyle.Render("Installed and configured successfully"))
						} else if strings.HasPrefix(status, "Failed:") {
							fmt.Fprintf(out, "  %s  %-24s %s\n", iconError, boldStyle.Render(name), errorStyle.Render(status))
						} else {
							fmt.Fprintf(out, "  %s  %-24s %s\n", iconArrow, boldStyle.Render(name), subtleStyle.Render(status))
						}
					}
				})

				// Re-evaluate diagnostics after auto-healing
				report = doctor.RunDiagnostics()
				passed, warnings, errorsCount = countDiagnosticResults(report)
				fixes = collectFixSuggestions(report)

				if !opts.json {
					fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Post-Healing Diagnostic Report ─────────────────────────"))
					renderDiagnosticResults(out, report, fixes, passed, warnings, errorsCount)
				}
			}
		}

		if report.HasErrors() && os.Getenv("ORBIT_TESTBED") != "1" && os.Getenv("ORBIT_SKIP_PREFLIGHT") != "1" {
			emitter.Emit(session.StageInit, "failed", fmt.Sprintf("Pre-flight check failed with %d errors", errorsCount), nil)
			if opts.nonInteractive {
				return fmt.Errorf("pre-flight diagnostics failed with %d error(s)", errorsCount)
			}
			if !promptYesNo(in, out, "Critical checks failed. Proceed anyway?", false) {
				return errors.New("onboarding cancelled due to pre-flight diagnostic errors")
			}
		}

		s.CurrentStage = session.StageDoctorPassed
		if err := sm.SaveSession(s); err != nil {
			return err
		}
		emitter.Emit(session.StageDoctorPassed, "completed", "Pre-flight diagnostics passed", nil)
	} else if !opts.json {
		fmt.Fprintf(out, "  %s  Pre-flight diagnostics already passed (cached checkpoint)\n", iconOK)
	}

	// ── STATE 2: Keypair Detection & Generation ────────────────────────────────
	if !isStageCompleted(s.CurrentStage, session.StageKeypairReady) {
		emitter.Emit(session.StageKeypairReady, "in_progress", "Configuring SSH keypair", nil)
		if !opts.json {
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Step 2: SSH Developer Keypair ──────────────────────────"))
		}

		pubKey, err := ensureSSHKey(opts.sshDir, s.Email)
		if err != nil {
			emitter.Emit(session.StageKeypairReady, "failed", err.Error(), nil)
			return fmt.Errorf("SSH keypair configuration failed: %w", err)
		}

		s.SSHPublicKey = pubKey
		s.CurrentStage = session.StageKeypairReady
		if err := sm.SaveSession(s); err != nil {
			return err
		}

		keySnippet := pubKey
		if len(keySnippet) > 48 {
			keySnippet = keySnippet[:48] + "…"
		}
		if !opts.json {
			fmt.Fprintf(out, "  %s  SSH keypair ready: %s\n", iconOK, subtleStyle.Render(keySnippet))
		}
		emitter.Emit(session.StageKeypairReady, "completed", "SSH keypair ready", map[string]string{"public_key": keySnippet})
	} else if !opts.json {
		fmt.Fprintf(out, "  %s  SSH keypair ready (cached checkpoint)\n", iconOK)
	}

	// ── STATE 3: Invitation Token & Edge Provisioning Claim ────────────────────
	if !isStageCompleted(s.CurrentStage, session.StageTokenClaimed) {
		emitter.Emit(session.StageTokenClaimed, "in_progress", "Submitting onboarding claim", nil)
		if !opts.json {
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Step 3: Identity & Provisioning Claim ──────────────────"))
		}

		token := strings.TrimSpace(opts.token)
		if token == "" {
			token = s.InviteToken
		}

		if token == "" {
			if opts.nonInteractive {
				emitter.Emit(session.StageTokenClaimed, "failed", "Invite token required in non-interactive mode", nil)
				return errors.New("missing required invite token (--token)")
			}
			token = promptString(in, out, "Enter your Orbit onboarding invite token", "")
			if token == "" {
				return errors.New("invite token cannot be empty")
			}
		}

		edgeURL := opts.edgeURL
		if edgeURL == "" {
			if envURL := strings.TrimSpace(os.Getenv("ORBIT_SERVER")); envURL != "" {
				edgeURL = envURL
			} else {
				edgeURL = "http://localhost:8080"
			}
		}

		fingerprint := getMachineFingerprint()
		idempKey := invite.ComputeIdempotencyKey(token, fingerprint)

		claimReq := provisioner.ClaimRequest{
			InviteToken:        token,
			DesiredUID:         opts.uid,
			Email:              s.Email,
			DisplayName:        s.DisplayName,
			SSHPublicKey:       s.SSHPublicKey,
			MachineFingerprint: fingerprint,
		}

		if !opts.json {
			fmt.Fprintf(out, "  %s  Submitting claim to %s...\n", iconArrow, codeStyle.Render(edgeURL))
		}

		claimResp, err := sendClaimRequest(cmd.Context(), edgeURL, idempKey, claimReq)
		if err != nil {
			emitter.Emit(session.StageTokenClaimed, "failed", err.Error(), nil)
			return fmt.Errorf("onboarding claim failed: %w", err)
		}

		s.InviteToken = token
		s.UID = claimResp.User.UID
		s.Email = claimResp.User.Email
		s.DisplayName = claimResp.User.DisplayName
		s.ForgejoToken = claimResp.Credentials.ForgejoMCPToken
		s.WireGuardConfig = claimResp.Credentials.WireGuardConfig
		s.CurrentStage = session.StageTokenClaimed

		if err := sm.SaveSession(s); err != nil {
			return err
		}

		if !opts.json {
			fmt.Fprintf(out, "  %s  Identity provisioned for %s (%s)\n",
				iconOK,
				boldStyle.Render(s.UID),
				subtleStyle.Render(s.Email),
			)
			if claimResp.IdempotentReplay {
				fmt.Fprintf(out, "  %s  Reconnected via idempotent replay key\n", iconInfo)
			}
		}

		emitter.Emit(session.StageTokenClaimed, "completed", "Identity provisioned", map[string]interface{}{
			"uid":               s.UID,
			"email":             s.Email,
			"idempotent_replay": claimResp.IdempotentReplay,
		})
	} else if !opts.json {
		fmt.Fprintf(out, "  %s  Identity claimed: %s (cached checkpoint)\n", iconOK, boldStyle.Render(s.UID))
	}

	// ── STATE 4: Dev Network Validation & Dev CA Trust ────────────────────────
	if !isStageCompleted(s.CurrentStage, session.StageNetworkConfigured) {
		emitter.Emit(session.StageNetworkConfigured, "in_progress", "Configuring network and local dev certificates", nil)
		if !opts.json {
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Step 4: Network & Local TLS Setup ───────────────────────"))
		}

		// Save WireGuard config if provided
		if s.WireGuardConfig != "" {
			if wgPath, wgErr := saveWireGuardConfig(s.WireGuardConfig); wgErr == nil {
				if !opts.json {
					fmt.Fprintf(out, "  %s  WireGuard VPN profile saved to %s\n", iconOK, subtleStyle.Render(wgPath))
				}
			}
		}

		// Validate loopback / DNS resolution for local dev domain
		devDomain := "dev.manova.space"
		if addrs, err := net.LookupHost(devDomain); err == nil && len(addrs) > 0 {
			if !opts.json {
				fmt.Fprintf(out, "  %s  Local dev DNS: %s -> %s\n", iconOK, codeStyle.Render(devDomain), subtleStyle.Render(strings.Join(addrs, ", ")))
			}
		} else {
			if !opts.json {
				fmt.Fprintf(out, "  %s  Local dev DNS: %s not resolvable (configure /etc/hosts or VPN if offline)\n", iconInfo, codeStyle.Render(devDomain))
			}
		}

		// Dev CA trust logic
		if caddyPath, err := exec.LookPath("caddy"); err == nil {
			trustCmd := exec.CommandContext(cmd.Context(), caddyPath, "trust")
			if trustErr := trustCmd.Run(); trustErr == nil {
				if !opts.json {
					fmt.Fprintf(out, "  %s  Caddy local root CA trusted\n", iconOK)
				}
			} else if !opts.json {
				fmt.Fprintf(out, "  %s  Caddy root CA trust skipped (requires elevated privileges)\n", iconInfo)
			}
		}

		s.CurrentStage = session.StageNetworkConfigured
		if err := sm.SaveSession(s); err != nil {
			return err
		}
		emitter.Emit(session.StageNetworkConfigured, "completed", "Network and local TLS configured", nil)
	} else if !opts.json {
		fmt.Fprintf(out, "  %s  Network & TLS setup ready (cached checkpoint)\n", iconOK)
	}

	// ── STATE 5: Workspace Initialization & Repository Cloning ────────────────
	if !isStageCompleted(s.CurrentStage, session.StageReposCloned) {
		emitter.Emit(session.StageReposCloned, "in_progress", "Cloning workspace repositories", nil)
		if !opts.json {
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Step 5: Workspace & Repositories ───────────────────────"))
			fmt.Fprintf(out, "  Workspace Root: %s\n", subtleStyle.Render(workspaceRoot))
		}

		// Ensure directory hierarchy
		_ = migrate.EnsureWorkspaceDirs(workspaceRoot)

		manifestPath := findManifestPath(workspaceRoot, opts.manifest)
		if _, err := os.Stat(manifestPath); err == nil {
			m, loadErr := manifest.Load(manifestPath)
			if loadErr == nil {
				targets, resolveErr := m.ResolveRepos("core")
				if resolveErr == nil && len(targets) > 0 {
					if !opts.json {
						fmt.Fprintf(out, "  Cloning %d repositories from manifest...\n", len(targets))
					}
					results := orchestrator.CloneTargets(workspaceRoot, targets, 4, func(res orchestrator.CloneResult) {
						if !opts.json {
							if res.Success {
								if res.AlreadyExists {
									fmt.Fprintf(out, "    %s  %-24s %s\n", iconOK, res.Name, subtleStyle.Render("(already present)"))
								} else {
									fmt.Fprintf(out, "    %s  %-24s %s\n", iconOK, res.Name, successStyle.Render("(cloned)"))
								}
							} else {
								fmt.Fprintf(out, "    %s  %-24s %s\n", iconError, res.Name, errorStyle.Render(res.Error))
							}
						}
					})

					for _, r := range results {
						if r.Success {
							s.ClonedRepos = append(s.ClonedRepos, r.Path)
						}
					}
					var rels []string
					for _, t := range targets {
						rels = append(rels, t.Path)
					}
					if err := pullWorkspaceAssets(cmd.Context(), workspaceRoot, rels, false); err != nil && !opts.json {
						fmt.Fprintf(out, "    %s  Assets: %s\n", iconWarn, err.Error())
					}
				}
			}
		}

		s.WorkspacePath = workspaceRoot
		s.CurrentStage = session.StageReposCloned
		if err := sm.SaveSession(s); err != nil {
			return err
		}
		emitter.Emit(session.StageReposCloned, "completed", "Workspace repositories cloned", map[string]int{"cloned_repos": len(s.ClonedRepos)})
	} else if !opts.json {
		fmt.Fprintf(out, "  %s  Workspace repositories ready (cached checkpoint)\n", iconOK)
	}

	// ── STATE 6: Cursor MCP & IDE Integration ──────────────────────────────────
	if !isStageCompleted(s.CurrentStage, session.StageMCPConfigured) {
		emitter.Emit(session.StageMCPConfigured, "in_progress", "Configuring Cursor MCP environment", nil)
		if !opts.json {
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Step 6: Cursor MCP & IDE Integration ───────────────────"))
		}

		if err := configureMCPEnvironment(workspaceRoot, s.ForgejoToken, s.UID); err != nil {
			if !opts.json {
				fmt.Fprintf(out, "  %s  MCP environment configuration warning: %v\n", iconWarn, err)
			}
		} else if !opts.json {
			fmt.Fprintf(out, "  %s  Configured %s with Forgejo credentials\n", iconOK, codeStyle.Render(".cursor/mcp.env"))
			fmt.Fprintf(out, "  %s  Symlinked Cursor agent skills and workspace rules\n", iconOK)
		}

		home, _ := os.UserHomeDir()
		if home != "" {
			_ = configureShellEnvironment(home)
			if !opts.json {
				fmt.Fprintf(out, "  %s  Configured shell profiles (~/.zshrc, ~/.bashrc) with Go proxy/private settings\n", iconOK)
			}
		}

		s.CurrentStage = session.StageMCPConfigured
		if err := sm.SaveSession(s); err != nil {
			return err
		}
		emitter.Emit(session.StageMCPConfigured, "completed", "MCP environment configured", nil)
	} else if !opts.json {
		fmt.Fprintf(out, "  %s  Cursor MCP environment ready (cached checkpoint)\n", iconOK)
	}

	// ── STATE 7: Local Dev Stack ───────────────────────────────────────────────
	if !isStageCompleted(s.CurrentStage, session.StageDevStackReady) {
		emitter.Emit(session.StageDevStackReady, "in_progress", "Checking dev stack startup", nil)
		if !opts.json {
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Step 7: Local Development Stack ────────────────────────"))
		}

		shouldStart := opts.startStack
		if !shouldStart && !opts.skipStack && !opts.nonInteractive {
			shouldStart = promptYesNo(in, out, "Would you like to start the local dev stack now (orbit dev up)?", true)
		}

		if shouldStart {
			if !opts.json {
				fmt.Fprintf(out, "  Starting local containers...\n")
			}
			infraDir := findOrbitInfraDir(workspaceRoot)
			if fi, err := os.Stat(infraDir); err == nil && fi.IsDir() {
				_ = runInInfra("docker", "compose", "up", "-d")
			}
		} else if !opts.json {
			fmt.Fprintf(out, "  %s  Skipped dev stack startup (start later with %s)\n", iconInfo, codeStyle.Render("orbit dev up"))
		}

		s.CurrentStage = session.StageDevStackReady
		s.CurrentStage = session.StageCompleted
		if err := sm.SaveSession(s); err != nil {
			return err
		}
		emitter.Emit(session.StageCompleted, "completed", "Onboarding completed successfully", s)
	}

	// ── Summary Card ───────────────────────────────────────────────────────────
	if !opts.json {
		var extraDetails string
		totpVal := ""
		if s.Metadata != nil {
			for _, k := range []string{"totp_uri", "otpauth", "totp_secret", "totp"} {
				if v, ok := s.Metadata[k]; ok && v != "" {
					totpVal = v
					break
				}
			}
		}
		if totpVal != "" {
			extraDetails = fmt.Sprintf("\nAuthelia 2FA:   %s\n                %s",
				infoStyle.Render("TOTP Enrollment Ready"),
				subtleStyle.Render(totpVal),
			)
		}

		summaryText := fmt.Sprintf(
			"Developer ID:   %s\n"+
				"Email:          %s\n"+
				"Workspace:      %s\n"+
				"Cursor MCP:     %s\n"+
				"VPN Profile:    %s%s\n\n"+
				"Next Steps:\n"+
				"  1. Launch Cursor in this workspace:\n"+
				"     %s\n"+
				"  2. Check development environment status:\n"+
				"     %s\n"+
				"  3. Open Developer Portal:\n"+
				"     %s",
			boldStyle.Render(s.UID),
			subtleStyle.Render(s.Email),
			subtleStyle.Render(workspaceRoot),
			successStyle.Render(".cursor/mcp.env (configured)"),
			infoStyle.Render("~/.config/orbit/wg0.conf"),
			extraDetails,
			codeStyle.Render("cursor "+workspaceRoot),
			codeStyle.Render("orbit status"),
			codeStyle.Render("orbit dev portal"),
		)
		fmt.Fprintln(out, renderCard("ONBOARDING COMPLETE", summaryText))
	}

	return nil
}

func ensureSSHKey(customSSHDir string, email string) (string, error) {
	sshDir := customSSHDir
	if sshDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home dir: %w", err)
		}
		sshDir = filepath.Join(home, ".ssh")
	}

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create ssh directory: %w", err)
	}

	// Check candidate keys
	candidates := []string{
		filepath.Join(sshDir, "id_ed25519.pub"),
		filepath.Join(sshDir, "id_rsa.pub"),
		filepath.Join(sshDir, "id_ecdsa.pub"),
	}

	for _, pubPath := range candidates {
		if data, err := os.ReadFile(pubPath); err == nil {
			keyStr := strings.TrimSpace(string(data))
			if keyStr != "" {
				privPath := strings.TrimSuffix(pubPath, ".pub")
				tryLoadSSHAgent(privPath)
				return keyStr, nil
			}
		}
	}

	// Generate new ed25519 key
	keyPath := filepath.Join(sshDir, "id_ed25519")
	pubKeyPath := keyPath + ".pub"

	comment := email
	if comment == "" {
		comment = "orbit-developer"
	}

	// Try ssh-keygen first if available
	keygenSuccess := false
	if _, err := exec.LookPath("ssh-keygen"); err == nil {
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-C", comment, "-f", keyPath, "-N", "")
		if err := cmd.Run(); err == nil {
			keygenSuccess = true
		}
	}

	// Fallback to Go crypto if ssh-keygen not available or failed
	if !keygenSuccess {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", fmt.Errorf("failed to generate ed25519 key: %w", err)
		}

		privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return "", fmt.Errorf("failed to marshal private key: %w", err)
		}
		privBlock := &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privBytes,
		}
		if err := os.WriteFile(keyPath, pem.EncodeToMemory(privBlock), 0600); err != nil {
			return "", fmt.Errorf("failed to write private key: %w", err)
		}

		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			return "", fmt.Errorf("failed to format ssh public key: %w", err)
		}
		pubLine := fmt.Sprintf("%s %s\n", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), comment)
		if err := os.WriteFile(pubKeyPath, []byte(pubLine), 0644); err != nil {
			return "", fmt.Errorf("failed to write public key: %w", err)
		}
	}

	data, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read generated public key: %w", err)
	}

	tryLoadSSHAgent(keyPath)
	return strings.TrimSpace(string(data)), nil
}

func tryLoadSSHAgent(privKeyPath string) {
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		_ = exec.Command("ssh-add", privKeyPath).Run()
	}
}

func sendClaimRequest(ctx context.Context, edgeURL, idempKey string, req provisioner.ClaimRequest) (*provisioner.ClaimResponse, error) {
	url := strings.TrimRight(edgeURL, "/") + "/api/v1/onboard/claim"
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize claim request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if idempKey != "" {
		httpReq.Header.Set("Idempotency-Key", idempKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to reach onboarding edge at %s: %w", url, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
			Code  int    `json:"code"`
		}
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("claim rejected (%d): %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("claim rejected with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var claimResp provisioner.ClaimResponse
	if err := json.Unmarshal(bodyBytes, &claimResp); err != nil {
		return nil, fmt.Errorf("failed to parse claim response: %w", err)
	}

	return &claimResp, nil
}

func configureMCPEnvironment(workspaceRoot string, token, uid string) error {
	cursorDir := filepath.Join(workspaceRoot, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return fmt.Errorf("failed to create .cursor directory: %w", err)
	}

	mcpEnvPath := filepath.Join(cursorDir, "mcp.env")
	existingContent := ""
	if data, err := os.ReadFile(mcpEnvPath); err == nil {
		existingContent = string(data)
	} else {
		_ = migrate.SetupMCPEnvironment(workspaceRoot)
		if data, err := os.ReadFile(mcpEnvPath); err == nil {
			existingContent = string(data)
		}
	}

	lines := strings.Split(existingContent, "\n")
	foundToken := false
	foundMCPToken := false
	foundUID := false
	var newLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FORGEJO_TOKEN=") {
			newLines = append(newLines, fmt.Sprintf("FORGEJO_TOKEN=%s", token))
			foundToken = true
		} else if strings.HasPrefix(trimmed, "FORGEJO_MCP_TOKEN=") {
			newLines = append(newLines, fmt.Sprintf("FORGEJO_MCP_TOKEN=%s", token))
			foundMCPToken = true
		} else if strings.HasPrefix(trimmed, "MANOVA_USER_UID=") {
			newLines = append(newLines, fmt.Sprintf("MANOVA_USER_UID=%s", uid))
			foundUID = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !foundToken && token != "" {
		newLines = append(newLines, fmt.Sprintf("FORGEJO_TOKEN=%s", token))
	}
	if !foundMCPToken && token != "" {
		newLines = append(newLines, fmt.Sprintf("FORGEJO_MCP_TOKEN=%s", token))
	}
	if !foundUID && uid != "" {
		newLines = append(newLines, fmt.Sprintf("MANOVA_USER_UID=%s", uid))
	}

	finalData := strings.Join(newLines, "\n")
	if !strings.HasSuffix(finalData, "\n") {
		finalData += "\n"
	}

	if err := os.WriteFile(mcpEnvPath, []byte(finalData), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", mcpEnvPath, err)
	}

	_ = migrate.SymlinkCursorRules(workspaceRoot)
	return nil
}

func saveWireGuardConfig(configData string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	wgDir := filepath.Join(home, ".config", "orbit")
	if err := os.MkdirAll(wgDir, 0700); err != nil {
		return "", err
	}
	wgPath := filepath.Join(wgDir, "wg0.conf")
	if err := os.WriteFile(wgPath, []byte(configData), 0600); err != nil {
		return "", err
	}
	return wgPath, nil
}

func configureShellEnvironment(homeDir string) error {
	if homeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home dir: %w", err)
		}
		homeDir = home
	}

	rcFiles := []string{".zshrc", ".bashrc"}
	goprivateLine := `export GOPRIVATE="git.dev.manova.space/*"`
	goproxyLine := `export GOPROXY="https://proxy.golang.org,direct"`

	for _, rc := range rcFiles {
		rcPath := filepath.Join(homeDir, rc)
		var existingContent string
		if data, err := os.ReadFile(rcPath); err == nil {
			existingContent = string(data)
		}

		var additions []string
		if !strings.Contains(existingContent, "GOPRIVATE=") && !strings.Contains(existingContent, "git.dev.manova.space") {
			additions = append(additions, goprivateLine)
		}
		if !strings.Contains(existingContent, "GOPROXY=") {
			additions = append(additions, goproxyLine)
		}

		if len(additions) > 0 {
			newContent := existingContent
			if newContent != "" && !strings.HasSuffix(newContent, "\n") {
				newContent += "\n"
			}
			newContent += strings.Join(additions, "\n") + "\n"
			if err := os.WriteFile(rcPath, []byte(newContent), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", rcPath, err)
			}
		}
	}
	return nil
}

func generateDiagnosticBundle(workspaceRoot string, sm *session.SessionManager, customOutPath string) (string, error) {
	var outPath string
	if customOutPath != "" && customOutPath != "default" {
		outPath = customOutPath
	} else {
		timestamp := time.Now().UTC().Format("20060102-150405")
		outPath = fmt.Sprintf("orbit-diag-%s.tar.gz", timestamp)
	}

	outDir := filepath.Dir(outPath)
	if outDir != "." && outDir != "" {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory for bundle: %w", err)
		}
	}

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("failed to create diagnostic bundle file %s: %w", outPath, err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// 1. Doctor report
	report := doctor.RunDiagnostics()
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if err := addTarFile(tw, "doctor_report.json", reportJSON); err != nil {
		return "", err
	}

	// 2. Sanitized session
	var sessData []byte
	if sm != nil {
		s, err := sm.LoadSession()
		if err == nil && s != nil {
			sanitized := *s
			if sanitized.InviteToken != "" {
				if len(sanitized.InviteToken) > 16 {
					sanitized.InviteToken = sanitized.InviteToken[:10] + "..." + sanitized.InviteToken[len(sanitized.InviteToken)-4:]
				} else {
					sanitized.InviteToken = "orbit-inv.***"
				}
			}
			if sanitized.ForgejoToken != "" {
				sanitized.ForgejoToken = "fjo_tok_***"
			}
			if sanitized.WireGuardConfig != "" {
				lines := strings.Split(sanitized.WireGuardConfig, "\n")
				for i, l := range lines {
					if strings.HasPrefix(strings.TrimSpace(l), "PrivateKey") {
						lines[i] = "PrivateKey = [REDACTED]"
					}
				}
				sanitized.WireGuardConfig = strings.Join(lines, "\n")
			}
			sessData, _ = json.MarshalIndent(sanitized, "", "  ")
		}
	}
	if len(sessData) == 0 {
		sessData = []byte("{\n  \"status\": \"no_pending_session\"\n}\n")
	}
	if err := addTarFile(tw, "session_sanitized.json", sessData); err != nil {
		return "", err
	}

	// 3. System info
	hostname, _ := os.Hostname()
	sysInfo := map[string]interface{}{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"go_version": runtime.Version(),
		"num_cpu":    runtime.NumCPU(),
		"hostname":   hostname,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	sysInfoJSON, _ := json.MarshalIndent(sysInfo, "", "  ")
	if err := addTarFile(tw, "system_info.json", sysInfoJSON); err != nil {
		return "", err
	}

	// 4. Sanitized .cursor/mcp.env
	mcpEnvPath := filepath.Join(workspaceRoot, ".cursor", "mcp.env")
	if envData, err := os.ReadFile(mcpEnvPath); err == nil {
		sanitizedEnv := sanitizeEnvContent(string(envData))
		if err := addTarFile(tw, "mcp_env_sanitized.txt", []byte(sanitizedEnv)); err != nil {
			return "", err
		}
	}

	// 5. Summary
	var summary strings.Builder
	summary.WriteString("=== MANOVA ONBOARDING DIAGNOSTIC BUNDLE ===\n")
	summary.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format(time.RFC3339)))
	summary.WriteString(fmt.Sprintf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	summary.WriteString(fmt.Sprintf("Go Version: %s\n", runtime.Version()))
	summary.WriteString(fmt.Sprintf("Doctor Checks: %d, Errors: %v, Warnings: %v\n", len(report.Results), report.HasErrors(), report.HasWarnings()))
	if err := addTarFile(tw, "summary.txt", []byte(summary.String())); err != nil {
		return "", err
	}

	return outPath, nil
}

func sanitizeEnvContent(content string) string {
	lines := strings.Split(content, "\n")
	sensitiveKeys := []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "PRIVATE", "AUTH"}
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			result = append(result, line)
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.ToUpper(strings.TrimSpace(parts[0]))
			isSensitive := false
			for _, sk := range sensitiveKeys {
				if strings.Contains(key, sk) {
					isSensitive = true
					break
				}
			}
			if isSensitive {
				result = append(result, fmt.Sprintf("%s=[REDACTED]", parts[0]))
			} else {
				result = append(result, line)
			}
		} else {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

func addTarFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0600,
		Size:    int64(len(data)),
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("failed to write tar body for %s: %w", name, err)
	}
	return nil
}

func executeRollback(ctx context.Context, workspaceRoot, edgeURL string, sm *session.SessionManager, out io.Writer) error {
	s, err := sm.LoadSession()
	if err != nil {
		return fmt.Errorf("failed to load session for rollback: %w", err)
	}
	if s == nil {
		fmt.Fprintln(out, infoStyle.Render("No active onboarding session found to rollback."))
		return nil
	}

	fmt.Fprintln(out, titleStyle.Render("Rolling Back Onboarding State..."))

	// 1. Rollback remote provisioner if UID and edgeURL are present
	if s.UID != "" && edgeURL != "" {
		url := fmt.Sprintf("%s/v1/onboard/rollback/%s", strings.TrimRight(edgeURL, "/"), s.UID)
		httpReq, _ := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		client := &http.Client{Timeout: 5 * time.Second}
		_, _ = client.Do(httpReq)
	}

	// 2. Remove cloned repos recorded in session
	for _, repo := range s.ClonedRepos {
		repoPath := repo
		if !filepath.IsAbs(repoPath) {
			repoPath = filepath.Join(workspaceRoot, repo)
		}
		if fi, err := os.Stat(repoPath); err == nil && fi.IsDir() {
			fmt.Fprintf(out, "  %s  Removing repository %s\n", iconOK, subtleStyle.Render(repo))
			_ = os.RemoveAll(repoPath)
		}
	}

	// 3. Remove WireGuard config if created
	home, _ := os.UserHomeDir()
	if home != "" {
		wgPath := filepath.Join(home, ".config", "orbit", "wg0.conf")
		if _, err := os.Stat(wgPath); err == nil {
			fmt.Fprintf(out, "  %s  Removing WireGuard config %s\n", iconOK, subtleStyle.Render(wgPath))
			_ = os.Remove(wgPath)
		}
	}

	// 4. Remove .cursor/mcp.env if created
	mcpEnvPath := filepath.Join(workspaceRoot, ".cursor", "mcp.env")
	if _, err := os.Stat(mcpEnvPath); err == nil {
		fmt.Fprintf(out, "  %s  Removing %s\n", iconOK, subtleStyle.Render(mcpEnvPath))
		_ = os.Remove(mcpEnvPath)
	}

	// 5. Clear session
	if err := sm.ClearSession(); err != nil {
		return fmt.Errorf("failed to clear session file: %w", err)
	}

	fmt.Fprintf(out, "\n%s  Rollback completed successfully.\n", iconOK)
	return nil
}

func executeReset(sm *session.SessionManager, out io.Writer) error {
	if err := sm.ClearSession(); err != nil {
		return fmt.Errorf("failed to reset session: %w", err)
	}
	fmt.Fprintln(out, successStyle.Render("✔ Onboarding session cleared. Ready for fresh onboarding."))
	return nil
}

func promptYesNo(r io.Reader, w io.Writer, question string, defaultYes bool) bool {
	var defStr string
	if defaultYes {
		defStr = "[Y/n]"
	} else {
		defStr = "[y/N]"
	}
	fmt.Fprintf(w, "%s %s: ", question, subtleStyle.Render(defStr))
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		text := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if text == "" {
			return defaultYes
		}
		return text == "y" || text == "yes"
	}
	return defaultYes
}

func promptString(r io.Reader, w io.Writer, question, defaultValue string) string {
	if defaultValue != "" {
		fmt.Fprintf(w, "%s %s: ", question, subtleStyle.Render("("+defaultValue+")"))
	} else {
		fmt.Fprintf(w, "%s: ", question)
	}
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return defaultValue
		}
		return text
	}
	return defaultValue
}

func getMachineFingerprint() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%s-%s", hostname, runtime.GOOS, runtime.GOARCH)
}

func countDiagnosticResults(report *doctor.DoctorReport) (passed, warnings, errors int) {
	if report == nil {
		return 0, 0, 0
	}
	for _, res := range report.Results {
		switch res.Status {
		case doctor.StatusOK:
			passed++
		case doctor.StatusWarning:
			warnings++
		case doctor.StatusError:
			errors++
		}
	}
	return passed, warnings, errors
}

func collectFixSuggestions(report *doctor.DoctorReport) []doctor.DiagnosticResult {
	if report == nil {
		return nil
	}
	var fixes []doctor.DiagnosticResult
	for _, res := range report.Results {
		if res.FixSuggestion != "" && res.Status != doctor.StatusOK {
			fixes = append(fixes, res)
		}
	}
	return fixes
}

func renderDiagnosticResults(out io.Writer, report *doctor.DoctorReport, fixes []doctor.DiagnosticResult, passed, warnings, errorsCount int) {
	for _, res := range report.Results {
		var icon string
		switch res.Status {
		case doctor.StatusOK:
			icon = iconOK
		case doctor.StatusWarning:
			icon = iconWarn
		case doctor.StatusError:
			icon = iconError
		default:
			icon = iconInfo
		}
		fmt.Fprintf(out, "  %s  %-26s %s\n", icon, res.Name, res.Message)
	}

	if len(fixes) > 0 {
		fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Diagnostic Fix Suggestions ─────────────────────────────"))
		for _, fix := range fixes {
			var badge string
			if fix.Status == doctor.StatusError {
				badge = errorStyle.Render("[ERROR]")
			} else {
				badge = warningStyle.Render("[WARN]")
			}
			fmt.Fprintf(out, "  %s %s: %s\n     %s %s\n",
				badge,
				boldStyle.Render(fix.Name),
				fix.Message,
				iconArrow,
				codeStyle.Render(fix.FixSuggestion),
			)
		}
	}

	fmt.Fprintf(out, "\n  %s  %s  %s\n",
		successStyle.Render(fmt.Sprintf("✔ %d passed", passed)),
		warningStyle.Render(fmt.Sprintf("⚠ %d warnings", warnings)),
		errorStyle.Render(fmt.Sprintf("✖ %d errors", errorsCount)),
	)
}
