package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	invitedomain "github.com/manovaspace/orbit-cli/pkg/invite/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

// Result is what one onboard invocation produced. It is the `data` object of the
// `--json` envelope; Events carries the progress events the command historically
// streamed, keeping their field shape.
type Result struct {
	SessionID   string                 `json:"sessionId,omitempty"`
	Stage       domain.Stage           `json:"stage"`
	UID         string                 `json:"uid,omitempty"`
	Email       string                 `json:"email,omitempty"`
	DisplayName string                 `json:"displayName,omitempty"`
	Workspace   string                 `json:"workspace,omitempty"`
	ClonedRepos []string               `json:"clonedRepos,omitempty"`
	DryRun      bool                   `json:"dryRun,omitempty"`
	Reset       bool                   `json:"reset,omitempty"`
	RolledBack  bool                   `json:"rolledBack,omitempty"`
	BundlePath  string                 `json:"bundlePath,omitempty"`
	Events      []domain.ProgressEvent `json:"events"`
}

// DefaultEdgeURL is used when neither --edge-url nor $ORBIT_SERVER is set.
const DefaultEdgeURL = "http://localhost:10122"

// Run executes one onboard invocation: a one-shot maintenance mode, the
// diagnostic bundle, or the headless step flow.
func (s *Service) Run(ctx context.Context, opts domain.Options) (Result, error) {
	s.UI = s.UI.withDefaults()
	res := Result{Events: []domain.ProgressEvent{}}

	workspaceRoot := s.Layout.WorkspaceRoot(opts.Workspace)
	res.Workspace = workspaceRoot

	if opts.DiscardCheckpoint() {
		s.emit(&res, domain.StageInit, domain.StatusCompleted, "Clearing session state", nil)
		return s.reset(&res)
	}
	if opts.Rollback {
		s.emit(&res, domain.StageInit, domain.StatusCompleted, "Rolling back onboarding state", nil)
		return s.rollback(ctx, workspaceRoot, opts.EdgeURL, &res)
	}
	if opts.DiagBundle != "" {
		return s.diagnosticBundle(workspaceRoot, opts, &res)
	}

	if !opts.JSON {
		fmt.Fprintln(s.out(), s.UI.Title(`
╔══════════════════════════════════════════════════════════════╗
║                  ORBIT DEVELOPER WIZARD                      ║
║         Zero-Leak Production Onboarding & Dev Stack          ║
╚══════════════════════════════════════════════════════════════╝`))
	}

	sess, err := s.inception(opts, &res)
	if err != nil {
		return res, err
	}

	if err := s.stepDiagnostics(ctx, opts, sess, &res); err != nil {
		return res, err
	}
	if opts.DryRun {
		return s.result(res, sess), nil
	}
	if err := s.stepKeypair(opts, sess, &res); err != nil {
		return res, err
	}
	if err := s.stepClaim(ctx, opts, sess, &res); err != nil {
		return res, err
	}
	if err := s.stepNetwork(ctx, opts, sess, &res); err != nil {
		return res, err
	}
	if err := s.stepWorkspace(ctx, opts, workspaceRoot, sess, &res); err != nil {
		return res, err
	}
	if err := s.stepMCP(opts, workspaceRoot, sess, &res); err != nil {
		return res, err
	}
	if err := s.stepStack(ctx, opts, workspaceRoot, sess, &res); err != nil {
		return res, err
	}

	if !opts.JSON {
		s.renderSummary(workspaceRoot, sess)
	}
	return s.result(res, sess), nil
}

// inception loads or creates the session checkpoint and resolves the
// resume/discard answered on an existing one.
func (s *Service) inception(opts domain.Options, res *Result) (*domain.SessionState, error) {
	var sess *domain.SessionState
	if s.Sessions.HasPending() {
		loaded, loadErr := s.Sessions.Load()
		if loadErr == nil && loaded != nil {
			switch {
			case opts.Resume:
				sess = loaded
				if !opts.JSON {
					fmt.Fprintf(s.out(), "  %s  Resuming existing session %s from stage: %s\n\n",
						s.UI.OK,
						s.UI.Code(sess.ID),
						s.UI.InfoText(string(sess.CurrentStage)),
					)
				}
				s.emit(res, sess.CurrentStage, domain.StatusResumed,
					fmt.Sprintf("Resuming session %s from stage %s", sess.ID, sess.CurrentStage), sessionDetails(sess))
			case !opts.NonInteractive && !opts.JSON:
				fmt.Fprintf(s.out(), "  %s  Found incomplete onboarding session (%s, stage: %s).\n",
					s.UI.Info,
					s.UI.Code(loaded.ID),
					s.UI.Warning(string(loaded.CurrentStage)),
				)
				if s.promptYesNo("Would you like to resume this session? (Enter 'n' to discard checkpoint)", true) {
					sess = loaded
					opts.Resume = true
					s.emit(res, sess.CurrentStage, domain.StatusResumed, fmt.Sprintf("Resuming session %s", sess.ID), sessionDetails(sess))
				} else {
					_ = s.Sessions.Clear()
					sess = s.Sessions.Create(opts.Email, opts.Name)
					_ = s.Sessions.Save(sess)
					fmt.Fprintf(s.out(), "  %s  Session checkpoint discarded. To automate starting fresh next time, use:\n     %s\n\n",
						s.UI.OK,
						s.UI.Code("orbit onboard --ignore-and-remove-checkpoint"),
					)
					s.emit(res, domain.StageInit, domain.StatusStarted, "Starting fresh onboarding session", sessionDetails(sess))
				}
			default:
				// Non-interactive, JSON or dry-run: a saved session only resumes
				// when asked, otherwise it is replaced unless a token would be
				// silently thrown away.
				if !opts.DryRun && opts.Token != "" && !opts.Resume && !opts.DiscardCheckpoint() {
					return nil, clierr.Failed(fmt.Sprintf("ongoing onboarding session (%s) detected; pass '--resume' to continue or '--ignore-and-remove-checkpoint' to discard checkpoint", loaded.ID))
				}
				_ = s.Sessions.Clear()
				sess = s.Sessions.Create(opts.Email, opts.Name)
				_ = s.Sessions.Save(sess)
			}
		}
	}

	if sess == nil {
		sess = s.Sessions.Create(opts.Email, opts.Name)
		_ = s.Sessions.Save(sess)
		s.emit(res, domain.StageInit, domain.StatusStarted, "Initialized onboarding session", sessionDetails(sess))
	}
	return sess, nil
}

func (s *Service) stepDiagnostics(ctx context.Context, opts domain.Options, sess *domain.SessionState, res *Result) error {
	if domain.IsStageCompleted(sess.CurrentStage, domain.StageDoctorPassed) {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Pre-flight diagnostics already passed (cached checkpoint)\n", s.UI.OK)
		}
		return nil
	}

	s.emit(res, domain.StageInit, domain.StatusInProgress, "Running pre-flight diagnostics", nil)
	if !opts.JSON {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerDiagnostics))
	}

	report := s.Diagnostics.Run()
	passed, warnings, errorsCount := countDiagnosticResults(report)
	fixes := collectFixSuggestions(report)

	if !opts.JSON {
		s.renderDiagnosticResults(report, fixes, passed, warnings, errorsCount)
	}

	if opts.DryRun {
		s.emit(res, domain.StageInit, domain.StatusCompleted, "Pre-flight dry run completed",
			map[string]int{"passed": passed, "errors": errorsCount})
		if !opts.JSON {
			fmt.Fprintln(s.out(), s.UI.Card("DRY-RUN PREVIEW", `
Actions that will be executed during onboarding:
  1. SSH Keypair: Detect or generate ed25519 key (~/.ssh/id_ed25519) and add to agent
  2. Identity Claim: Claim cryptographic invite token against edge provisioner
  3. WireGuard VPN: Configure local VPN profile for access to dev services
  4. Repositories: Clone core workspace repositories from workspace.yaml
  5. Cursor IDE & MCP: Configure .cursor/mcp.env and link workspace agent rules
  6. Dev Stack: Optionally start local Docker containers (orbit dev up)
`))
		}
		res.DryRun = true
		res.Stage = sess.CurrentStage
		return nil
	}

	healable := s.Diagnostics.HealableCount(report.Results)
	if healable > 0 {
		shouldHeal := opts.AutoFix
		if !shouldHeal {
			switch {
			case opts.NonInteractive:
				// -y / --yes / --non-interactive heals without asking.
				shouldHeal = true
			case s.TTY || !s.StdinIsOS:
				if !opts.JSON {
					fmt.Fprintln(s.out())
				}
				shouldHeal = s.promptYesNo("? Automatically install missing prerequisites to continue?", true)
			}
		}

		if shouldHeal {
			s.emit(res, domain.StageInit, domain.StatusInProgress, "Auto-healing missing prerequisites",
				map[string]int{"healable_count": healable})
			if !opts.JSON {
				fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerHealing))
			}

			_ = s.Diagnostics.Heal(ctx, report.Results, func(name, status string) {
				if opts.JSON {
					return
				}
				switch {
				case status == "Completed successfully":
					fmt.Fprintf(s.out(), "  %s  %-24s %s\n", s.UI.OK, s.UI.Bold(name), s.UI.Success("Installed and configured successfully"))
				case strings.HasPrefix(status, "Failed:"):
					fmt.Fprintf(s.out(), "  %s  %-24s %s\n", s.UI.Error, s.UI.Bold(name), s.UI.ErrorText(status))
				default:
					fmt.Fprintf(s.out(), "  %s  %-24s %s\n", s.UI.Arrow, s.UI.Bold(name), s.UI.Subtle(status))
				}
			})

			// Re-evaluate diagnostics after auto-healing.
			report = s.Diagnostics.Run()
			passed, warnings, errorsCount = countDiagnosticResults(report)
			fixes = collectFixSuggestions(report)

			if !opts.JSON {
				fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerPostHealing))
				s.renderDiagnosticResults(report, fixes, passed, warnings, errorsCount)
			}
		}
	}

	if report.HasErrors() && s.Environment.Getenv("ORBIT_TESTBED") != "1" && s.Environment.Getenv("ORBIT_SKIP_PREFLIGHT") != "1" {
		s.emit(res, domain.StageInit, domain.StatusFailed, fmt.Sprintf("Pre-flight check failed with %d errors", errorsCount), nil)
		if opts.NonInteractive {
			return clierr.Failed(fmt.Sprintf("pre-flight diagnostics failed with %d error(s)", errorsCount))
		}
		if !s.promptYesNo("Critical checks failed. Proceed anyway?", false) {
			return clierr.Failed("onboarding cancelled due to pre-flight diagnostic errors")
		}
	}

	sess.CurrentStage = domain.StageDoctorPassed
	if err := s.Sessions.Save(sess); err != nil {
		return err
	}
	s.emit(res, domain.StageDoctorPassed, domain.StatusCompleted, "Pre-flight diagnostics passed", nil)
	return nil
}

func (s *Service) stepKeypair(opts domain.Options, sess *domain.SessionState, res *Result) error {
	if domain.IsStageCompleted(sess.CurrentStage, domain.StageKeypairReady) {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  SSH keypair ready (cached checkpoint)\n", s.UI.OK)
		}
		return nil
	}

	s.emit(res, domain.StageKeypairReady, domain.StatusInProgress, "Configuring SSH keypair", nil)
	if !opts.JSON {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerKeypair))
	}

	pubKey, err := s.SSH.EnsureKey(opts.SSHDir, sess.Email)
	if err != nil {
		s.emit(res, domain.StageKeypairReady, domain.StatusFailed, err.Error(), nil)
		return withCode(err, clierr.Tool("SSH keypair configuration failed"))
	}

	sess.SSHPublicKey = pubKey
	sess.CurrentStage = domain.StageKeypairReady
	if err := s.Sessions.Save(sess); err != nil {
		return err
	}

	keySnippet := pubKey
	if len(keySnippet) > 48 {
		keySnippet = keySnippet[:48] + "…"
	}
	if !opts.JSON {
		fmt.Fprintf(s.out(), "  %s  SSH keypair ready: %s\n", s.UI.OK, s.UI.Subtle(keySnippet))
	}
	s.emit(res, domain.StageKeypairReady, domain.StatusCompleted, "SSH keypair ready",
		map[string]string{"public_key": keySnippet})
	return nil
}

func (s *Service) stepClaim(ctx context.Context, opts domain.Options, sess *domain.SessionState, res *Result) error {
	if domain.IsStageCompleted(sess.CurrentStage, domain.StageTokenClaimed) {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Identity claimed: %s (cached checkpoint)\n", s.UI.OK, s.UI.Bold(sess.UID))
		}
		return nil
	}

	s.emit(res, domain.StageTokenClaimed, domain.StatusInProgress, "Submitting onboarding claim", nil)
	if !opts.JSON {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerClaim))
	}

	token := strings.TrimSpace(opts.Token)
	if token == "" {
		token = sess.InviteToken
	}

	if token == "" {
		if opts.NonInteractive || opts.JSON {
			s.emit(res, domain.StageTokenClaimed, domain.StatusFailed, "Invite token required in non-interactive mode", nil)
			return clierr.Usage("missing required invite token (--token)")
		}
		token = s.promptString("Enter your Orbit onboarding invite token", "")
		if token == "" {
			return clierr.Usage("invite token cannot be empty")
		}
	}

	edgeURL := s.edgeURL(opts)
	idempKey := invitedomain.ComputeIdempotencyKey(token, s.fingerprint())

	claimReq := domain.ClaimRequest{
		InviteToken:        token,
		DesiredUID:         opts.UID,
		Email:              sess.Email,
		DisplayName:        sess.DisplayName,
		SSHPublicKey:       sess.SSHPublicKey,
		MachineFingerprint: s.fingerprint(),
	}

	if !opts.JSON {
		fmt.Fprintf(s.out(), "  %s  Submitting claim to %s...\n", s.UI.Arrow, s.UI.Code(edgeURL))
	}

	claimResp, err := s.Provisioner.Claim(ctx, edgeURL, idempKey, claimReq)
	if err != nil {
		s.emit(res, domain.StageTokenClaimed, domain.StatusFailed, err.Error(), nil)
		return withCode(err, clierr.Failed("onboarding claim failed"))
	}

	sess.InviteToken = token
	sess.UID = claimResp.User.UID
	sess.Email = claimResp.User.Email
	sess.DisplayName = claimResp.User.DisplayName
	sess.ForgejoToken = claimResp.Credentials.ForgejoMCPToken
	sess.WireGuardConfig = claimResp.Credentials.WireGuardConfig
	sess.CurrentStage = domain.StageTokenClaimed

	if err := s.Sessions.Save(sess); err != nil {
		return err
	}

	if !opts.JSON {
		fmt.Fprintf(s.out(), "  %s  Identity provisioned for %s (%s)\n",
			s.UI.OK,
			s.UI.Bold(sess.UID),
			s.UI.Subtle(sess.Email),
		)
		if claimResp.IdempotentReplay {
			fmt.Fprintf(s.out(), "  %s  Reconnected via idempotent replay key\n", s.UI.Info)
		}
	}

	s.emit(res, domain.StageTokenClaimed, domain.StatusCompleted, "Identity provisioned", map[string]any{
		"uid":               sess.UID,
		"email":             sess.Email,
		"idempotent_replay": claimResp.IdempotentReplay,
	})
	return nil
}

func (s *Service) stepNetwork(ctx context.Context, opts domain.Options, sess *domain.SessionState, res *Result) error {
	if domain.IsStageCompleted(sess.CurrentStage, domain.StageNetworkConfigured) {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Network & TLS setup ready (cached checkpoint)\n", s.UI.OK)
		}
		return nil
	}

	s.emit(res, domain.StageNetworkConfigured, domain.StatusInProgress, "Configuring network and local dev certificates", nil)
	if !opts.JSON {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerNetwork))
	}

	if sess.WireGuardConfig != "" {
		if wgPath, wgErr := s.Environment.WriteWireGuard(sess.WireGuardConfig); wgErr == nil {
			if !opts.JSON {
				fmt.Fprintf(s.out(), "  %s  WireGuard VPN profile saved to %s\n", s.UI.OK, s.UI.Subtle(wgPath))
			}
		}
	}

	const devDomain = "internal.manova.space"
	if addrs, err := s.Environment.LookupHost(devDomain); err == nil && len(addrs) > 0 {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Local dev DNS: %s -> %s\n", s.UI.OK, s.UI.Code(devDomain), s.UI.Subtle(strings.Join(addrs, ", ")))
		}
	} else if !opts.JSON {
		fmt.Fprintf(s.out(), "  %s  Local dev DNS: %s not resolvable (configure /etc/hosts or VPN if offline)\n", s.UI.Info, s.UI.Code(devDomain))
	}

	if present, trusted := s.Environment.TrustDevCA(ctx); present {
		if trusted {
			if !opts.JSON {
				fmt.Fprintf(s.out(), "  %s  Caddy local root CA trusted\n", s.UI.OK)
			}
		} else if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Caddy root CA trust skipped (requires elevated privileges)\n", s.UI.Info)
		}
	}

	sess.CurrentStage = domain.StageNetworkConfigured
	if err := s.Sessions.Save(sess); err != nil {
		return err
	}
	s.emit(res, domain.StageNetworkConfigured, domain.StatusCompleted, "Network and local TLS configured", nil)
	return nil
}

func (s *Service) stepWorkspace(ctx context.Context, opts domain.Options, workspaceRoot string, sess *domain.SessionState, res *Result) error {
	if domain.IsStageCompleted(sess.CurrentStage, domain.StageReposCloned) {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Workspace repositories ready (cached checkpoint)\n", s.UI.OK)
		}
		return nil
	}

	s.emit(res, domain.StageReposCloned, domain.StatusInProgress, "Cloning workspace repositories", nil)
	if !opts.JSON {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerWorkspace))
		fmt.Fprintf(s.out(), "  Workspace Root: %s\n", s.UI.Subtle(workspaceRoot))
	}

	_ = s.Environment.EnsureWorkspaceDirs(workspaceRoot)

	manifestPath := s.Layout.ManifestPath(workspaceRoot, opts.Manifest)
	if s.Environment.Exists(manifestPath) {
		rels, err := s.Repos.CloneCore(ctx, workspaceRoot, manifestPath, func(count int) {
			if !opts.JSON {
				fmt.Fprintf(s.out(), "  Cloning %d repositories from manifest...\n", count)
			}
		}, func(worked workspacedomain.CloneResult) {
			if worked.Success {
				sess.ClonedRepos = append(sess.ClonedRepos, worked.Path)
			}
			if opts.JSON {
				return
			}
			switch {
			case !worked.Success:
				fmt.Fprintf(s.out(), "    %s  %-24s %s\n", s.UI.Error, worked.Name, s.UI.ErrorText(worked.Error))
			case worked.AlreadyExists:
				fmt.Fprintf(s.out(), "    %s  %-24s %s\n", s.UI.OK, worked.Name, s.UI.Subtle("(already present)"))
			default:
				fmt.Fprintf(s.out(), "    %s  %-24s %s\n", s.UI.OK, worked.Name, s.UI.Success("(cloned)"))
			}
		})
		if err == nil && len(rels) > 0 {
			if pullErr := s.Repos.PullAssets(ctx, workspaceRoot, rels); pullErr != nil && !opts.JSON {
				fmt.Fprintf(s.out(), "    %s  Assets: %s\n", s.UI.Warn, pullErr.Error())
			}
		}
	}

	sess.WorkspacePath = workspaceRoot
	sess.CurrentStage = domain.StageReposCloned
	if err := s.Sessions.Save(sess); err != nil {
		return err
	}
	s.emit(res, domain.StageReposCloned, domain.StatusCompleted, "Workspace repositories cloned",
		map[string]int{"cloned_repos": len(sess.ClonedRepos)})
	return nil
}

func (s *Service) stepMCP(opts domain.Options, workspaceRoot string, sess *domain.SessionState, res *Result) error {
	if domain.IsStageCompleted(sess.CurrentStage, domain.StageMCPConfigured) {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Cursor MCP environment ready (cached checkpoint)\n", s.UI.OK)
		}
		return nil
	}

	s.emit(res, domain.StageMCPConfigured, domain.StatusInProgress, "Configuring Cursor MCP environment", nil)
	if !opts.JSON {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerMCP))
	}

	if err := s.Environment.ConfigureMCPEnv(workspaceRoot, sess.ForgejoToken, sess.UID); err != nil {
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  MCP environment configuration warning: %v\n", s.UI.Warn, err)
		}
	} else if !opts.JSON {
		fmt.Fprintf(s.out(), "  %s  Configured %s with Forgejo credentials\n", s.UI.OK, s.UI.Code(".cursor/mcp.env"))
		fmt.Fprintf(s.out(), "  %s  Symlinked Cursor agent skills and workspace rules\n", s.UI.OK)
	}

	if home, _ := s.Environment.HomeDir(); home != "" {
		_ = s.Environment.ConfigureShell(home)
		if !opts.JSON {
			fmt.Fprintf(s.out(), "  %s  Configured shell profiles (~/.zshrc, ~/.bashrc) with Go proxy/private settings\n", s.UI.OK)
		}
	}

	sess.CurrentStage = domain.StageMCPConfigured
	if err := s.Sessions.Save(sess); err != nil {
		return err
	}
	s.emit(res, domain.StageMCPConfigured, domain.StatusCompleted, "MCP environment configured", nil)
	return nil
}

func (s *Service) stepStack(ctx context.Context, opts domain.Options, workspaceRoot string, sess *domain.SessionState, res *Result) error {
	if domain.IsStageCompleted(sess.CurrentStage, domain.StageDevStackReady) {
		return nil
	}

	s.emit(res, domain.StageDevStackReady, domain.StatusInProgress, "Checking dev stack startup", nil)
	if !opts.JSON {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header(headerStack))
	}

	shouldStart := opts.StartStack
	if !shouldStart && !opts.SkipStack && !opts.NonInteractive {
		shouldStart = s.promptYesNo("Would you like to start the local dev stack now (orbit dev up)?", true)
	}

	if shouldStart {
		if !opts.JSON {
			fmt.Fprintln(s.out(), "  Starting local containers...")
		}
		if infraDir := s.Layout.InfraDir(workspaceRoot); s.Environment.IsDir(infraDir) && s.Stack != nil {
			_ = s.Stack.Start(ctx, infraDir)
		}
	} else if !opts.JSON {
		fmt.Fprintf(s.out(), "  %s  Skipped dev stack startup (start later with %s)\n", s.UI.Info, s.UI.Code("orbit dev up"))
	}

	sess.CurrentStage = domain.StageDevStackReady
	sess.CurrentStage = domain.StageCompleted
	if err := s.Sessions.Save(sess); err != nil {
		return err
	}
	s.emit(res, domain.StageCompleted, domain.StatusCompleted, "Onboarding completed successfully", sessionDetails(sess))
	return nil
}

func (s *Service) reset(res *Result) (Result, error) {
	if err := s.Sessions.Clear(); err != nil {
		return *res, clierr.Wrap(err, clierr.Failed("failed to reset session"))
	}
	fmt.Fprintln(s.out(), s.UI.Success("✔ Onboarding session cleared. Ready for fresh onboarding."))
	res.Reset = true
	return *res, nil
}

func (s *Service) rollback(ctx context.Context, workspaceRoot, edgeURL string, res *Result) (Result, error) {
	sess, err := s.Sessions.Load()
	if err != nil {
		return *res, clierr.Wrap(err, clierr.Failed("failed to load session for rollback"))
	}
	if sess == nil {
		fmt.Fprintln(s.out(), s.UI.InfoText("No active onboarding session found to rollback."))
		return *res, nil
	}

	fmt.Fprintln(s.out(), s.UI.Title("Rolling Back Onboarding State..."))

	if sess.UID != "" && edgeURL != "" {
		_ = s.Provisioner.Rollback(ctx, edgeURL, sess.UID)
	}

	for _, repo := range sess.ClonedRepos {
		repoPath := repo
		if !filepath.IsAbs(repoPath) {
			repoPath = filepath.Join(workspaceRoot, repo)
		}
		if s.Environment.IsDir(repoPath) {
			fmt.Fprintf(s.out(), "  %s  Removing repository %s\n", s.UI.OK, s.UI.Subtle(repo))
			_ = s.Environment.Remove(repoPath)
		}
	}

	if home, _ := s.Environment.HomeDir(); home != "" {
		wgPath := filepath.Join(home, ".config", "orbit", "wg0.conf")
		if s.Environment.Exists(wgPath) {
			fmt.Fprintf(s.out(), "  %s  Removing WireGuard config %s\n", s.UI.OK, s.UI.Subtle(wgPath))
			_ = s.Environment.Remove(wgPath)
		}
	}

	mcpEnvPath := filepath.Join(workspaceRoot, ".cursor", "mcp.env")
	if s.Environment.Exists(mcpEnvPath) {
		fmt.Fprintf(s.out(), "  %s  Removing %s\n", s.UI.OK, s.UI.Subtle(mcpEnvPath))
		_ = s.Environment.Remove(mcpEnvPath)
	}

	if err := s.Sessions.Clear(); err != nil {
		return *res, clierr.Wrap(err, clierr.Failed("failed to clear session file"))
	}

	fmt.Fprintf(s.out(), "\n%s  Rollback completed successfully.\n", s.UI.OK)
	res.RolledBack = true
	res.Stage = sess.CurrentStage
	return *res, nil
}

func (s *Service) diagnosticBundle(workspaceRoot string, opts domain.Options, res *Result) (Result, error) {
	target := opts.DiagBundle
	if (target == "default" || target == "") && opts.BundleArg != "" {
		target = opts.BundleArg
	}

	s.emit(res, domain.StageInit, domain.StatusStarted, "Generating diagnostic bundle", nil)
	sess, _ := s.Sessions.Load()
	bundlePath, err := s.Bundle.Write(workspaceRoot, sess, target)
	if err != nil {
		s.emit(res, domain.StageInit, domain.StatusFailed, err.Error(), nil)
		return *res, err
	}
	s.emit(res, domain.StageInit, domain.StatusCompleted, fmt.Sprintf("Bundle generated: %s", bundlePath),
		map[string]string{"bundle_path": bundlePath})

	if !opts.JSON {
		fmt.Fprintln(s.out(), s.UI.Title("Orbit Diagnostic Bundle Generated"))
		fmt.Fprintf(s.out(), "  %s  Sanitized diagnostic archive created at:\n     %s\n\n",
			s.UI.OK,
			s.UI.Code(bundlePath),
		)
		fmt.Fprintf(s.out(), "  %s  Share this bundle with the platform team for triage.\n", s.UI.Info)
	}

	res.BundlePath = bundlePath
	res.Stage = domain.StageInit
	return *res, nil
}

// result fills the machine-readable summary from the finished session.
func (s *Service) result(res Result, sess *domain.SessionState) Result {
	res.SessionID = sess.ID
	res.Stage = sess.CurrentStage
	res.UID = sess.UID
	res.Email = sess.Email
	res.DisplayName = sess.DisplayName
	res.ClonedRepos = sess.ClonedRepos
	if res.Workspace == "" {
		res.Workspace = sess.WorkspacePath
	}
	return res
}

func (s *Service) edgeURL(opts domain.Options) string {
	if opts.EdgeURL != "" {
		return opts.EdgeURL
	}
	if envURL := strings.TrimSpace(s.Environment.Getenv("ORBIT_SERVER")); envURL != "" {
		return envURL
	}
	return DefaultEdgeURL
}

func (s *Service) fingerprint() string {
	goos, goarch := s.Environment.Platform()
	return domain.MachineFingerprint(s.Environment.Hostname(), goos, goarch)
}

func (s *Service) promptYesNo(question string, defaultYes bool) bool {
	def := "[Y/n]"
	if !defaultYes {
		def = "[y/N]"
	}
	return s.Prompts.YesNo(question, s.UI.Subtle(def), defaultYes)
}

func (s *Service) promptString(question, defaultValue string) string {
	suffix := ""
	if defaultValue != "" {
		suffix = s.UI.Subtle("(" + defaultValue + ")")
	}
	return s.Prompts.String(question, suffix, defaultValue)
}

func (s *Service) emit(res *Result, stage domain.Stage, status, message string, details any) {
	res.Events = append(res.Events, domain.ProgressEvent{
		Stage:     stage,
		Status:    status,
		Message:   message,
		Details:   details,
		Timestamp: s.now().UTC(),
	})
}

// sessionDetails is the secret-free view of a session that progress events may
// carry. The full session holds the invite token, the git token and the VPN
// private key, so it must never be marshalled into output.
func sessionDetails(sess *domain.SessionState) map[string]any {
	if sess == nil {
		return nil
	}
	return map[string]any{
		"session_id":   sess.ID,
		"stage":        sess.CurrentStage,
		"uid":          sess.UID,
		"email":        sess.Email,
		"display_name": sess.DisplayName,
		"workspace":    sess.WorkspacePath,
		"cloned_repos": len(sess.ClonedRepos),
	}
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// withCode prefixes msg with the error's clierr code when it has one.
func withCode(err error, fallback *clierr.Error) error {
	var c *clierr.Error
	if errors.As(err, &c) {
		return &clierr.Error{Code: c.Code, Exit: c.Exit, Message: fallback.Message, Hint: c.Hint, Err: err}
	}
	return clierr.Wrap(err, fallback)
}
