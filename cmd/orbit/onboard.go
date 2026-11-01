package main

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/manovaspace/orbit-cli/pkg/onboard/application"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/claimhttp"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/diagbundle"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/doctorprobe"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/hostfs"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/layout"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/prompts"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/repos"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/sessionstore"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/sshkeys"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/stack"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
	"github.com/spf13/cobra"
)

// onboardRunner is the seam through which onboarding reaches ssh-keygen,
// ssh-add, caddy, docker and the workspace bootstrapper.
var onboardRunner runner.Runner = runner.Exec{}

// onboardIsTTY is the seam that decides whether the interactive wizard owns the
// invocation.
var onboardIsTTY = istty.IsInteractiveSession

// onboardService is the seam through which command tests inject fake adapters.
var onboardService = newOnboardService

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
	cmd.Flags().StringVar(&opts.edgeURL, "edge-url", "", "Onboarding edge gateway URL (default: $ORBIT_SERVER or http://localhost:10122)")
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
	cmd.Flags().BoolVar(&opts.json, "json", false, "Print the result as a JSON envelope")
	cmd.Flags().BoolVar(&opts.skipStack, "skip-stack", false, "Skip local dev stack initialization")
	cmd.Flags().BoolVar(&opts.startStack, "start-stack", false, "Automatically start local dev stack without prompting")

	return cmd
}

func runOnboard(cmd *cobra.Command, args []string, opts *onboardOptions) error {
	out := cmd.OutOrStdout()

	// In JSON mode the envelope is the only thing stdout may carry, so cobra
	// must not append its own error text and usage block.
	if opts.json {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
	}

	input := domain.Options{
		Token:                     opts.token,
		Name:                      opts.name,
		Email:                     opts.email,
		UID:                       opts.uid,
		EdgeURL:                   opts.edgeURL,
		Workspace:                 opts.workspace,
		Manifest:                  opts.manifest,
		SessionFile:               opts.sessionFile,
		SSHDir:                    opts.sshDir,
		DiagBundle:                opts.diagBundle,
		DryRun:                    opts.dryRun,
		Resume:                    opts.resume,
		IgnoreAndRemoveCheckpoint: opts.ignoreAndRemoveCheckpoint,
		Reset:                     opts.reset,
		Rollback:                  opts.rollback,
		NonInteractive:            opts.nonInteractive,
		AutoFix:                   opts.autoFix,
		JSON:                      opts.json,
		SkipStack:                 opts.skipStack,
		StartStack:                opts.startStack,
	}
	if len(args) > 0 {
		input.BundleArg = args[0]
	}

	mode, err := domain.ResolveMode(input, onboardIsTTY())
	if err != nil {
		return onboardFailure(out, opts.json, clierr.Usage(err.Error()))
	}

	// The interactive wizard is the presentation adapter; it drives the same
	// session checkpoints the headless flow uses.
	if mode == domain.ModeWizard {
		sm, err := sessionstore.New(opts.sessionFile)
		if err != nil {
			return onboardFailure(out, opts.json, clierr.Wrap(err, clierr.Failed("failed to initialize session manager")))
		}
		return tuiOnboard.RunWizard(tuiOnboard.WizardOptions{
			SessionManager: sm,
			PreSetToken:    opts.token,
			Resume:         opts.resume,
			Reset:          opts.ignoreAndRemoveCheckpoint || opts.reset,
			Rollback:       opts.rollback,
		})
	}

	uiOut := io.Writer(out)
	if opts.json {
		uiOut = io.Discard
	}

	svc, err := onboardService(cmd, opts, uiOut)
	if err != nil {
		return onboardFailure(out, opts.json, err)
	}

	res, err := svc.Run(cmd.Context(), input)
	if err != nil {
		return onboardFailure(out, opts.json, err)
	}
	if opts.json {
		return output.Write(out, output.Success(res))
	}
	return nil
}

// onboardFailure emits the JSON error envelope when --json is set and always
// returns the error so the exit code follows clierr.
func onboardFailure(out io.Writer, jsonFlag bool, err error) error {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
	return err
}

// newOnboardService wires the application layer to the real adapters.
func newOnboardService(cmd *cobra.Command, opts *onboardOptions, out io.Writer) (*application.Service, error) {
	sessions, err := sessionstore.New(opts.sessionFile)
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to initialize session manager"))
	}

	probe := doctorprobe.New()
	repoService := repos.New()
	repoService.Assets = func(ctx context.Context, root string, rels []string) error {
		return pullWorkspaceAssets(ctx, root, rels, false)
	}

	return &application.Service{
		Provisioner: claimhttp.New(),
		Sessions:    sessions,
		Diagnostics: probe,
		SSH:         sshkeys.New(onboardRunner),
		Environment: hostfs.New(onboardRunner),
		Layout: layout.Layout{
			Root:     findWorkspaceRoot,
			Manifest: findManifestPath,
			Infra:    findOrbitInfraDir,
		},
		Repos:     repoService,
		Stack:     stack.New(onboardRunner),
		Bundle:    &diagbundle.Writer{Diagnostics: probe.Run},
		Prompts:   prompts.New(cmd.InOrStdin(), out),
		Out:       out,
		UI:        onboardUI(),
		Now:       time.Now,
		TTY:       onboardIsTTY(),
		StdinIsOS: cmd.InOrStdin() == os.Stdin,
	}, nil
}

// onboardUI carries the styles cmd/orbit renders so the application layer
// writes the same lines without importing lipgloss.
func onboardUI() application.UI {
	return application.UI{
		Arrow:     iconArrow,
		OK:        iconOK,
		Warn:      iconWarn,
		Error:     iconError,
		Info:      iconInfo,
		Title:     func(text string) string { return titleStyle.Render(text) },
		Header:    func(text string) string { return headerStyle.Render(text) },
		Bold:      func(text string) string { return boldStyle.Render(text) },
		Success:   func(text string) string { return successStyle.Render(text) },
		Warning:   func(text string) string { return warningStyle.Render(text) },
		ErrorText: func(text string) string { return errorStyle.Render(text) },
		InfoText:  func(text string) string { return infoStyle.Render(text) },
		Subtle:    func(text string) string { return subtleStyle.Render(text) },
		Code:      func(text string) string { return codeStyle.Render(text) },
		Card:      renderCard,
	}
}
