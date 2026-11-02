package domain

import "errors"

// ErrJSONRequiresHeadless is returned when --json is combined with the
// interactive-only path.
var ErrJSONRequiresHeadless = errors.New("--json cannot run the interactive wizard, which does not emit JSON: pass --non-interactive (or --dry-run) for a machine-readable envelope")

// Mode is how one onboard invocation runs.
type Mode string

const (
	// ModeWizard is the interactive Bubble Tea wizard.
	ModeWizard Mode = "wizard"
	// ModePrompted is a headless run that may still ask on stdin.
	ModePrompted Mode = "prompted"
	// ModeHeadless runs without asking anything.
	ModeHeadless Mode = "headless"
)

// Options is one `orbit onboard` invocation.
type Options struct {
	Token   string
	Name    string
	Email   string
	UID     string
	EdgeURL string

	Workspace   string
	Manifest    string
	SessionFile string
	SSHDir      string

	// DiagBundle is empty when the flag is absent, "default" when it was passed
	// without a value, and a filename otherwise.
	DiagBundle string
	// BundleArg is the positional argument used as the bundle name when
	// DiagBundle is "default".
	BundleArg string

	DryRun                    bool
	Resume                    bool
	IgnoreAndRemoveCheckpoint bool
	Reset                     bool
	Rollback                  bool
	NonInteractive            bool
	AutoFix                   bool
	JSON                      bool
	SkipStack                 bool
	StartStack                bool
}

// OneShot reports whether the invocation is a single maintenance action that
// never enters the step flow.
func (o Options) OneShot() bool {
	return o.Reset || o.IgnoreAndRemoveCheckpoint || o.Rollback || o.DiagBundle != ""
}

// DiscardCheckpoint reports whether a saved checkpoint must be thrown away.
func (o Options) DiscardCheckpoint() bool {
	return o.IgnoreAndRemoveCheckpoint || o.Reset
}

// ResolveMode decides how the command runs. `--json` is only valid where the
// wizard cannot take over: on an interactive terminal it needs
// --non-interactive or a one-shot mode, and the flow never prompts in wizard
// mode. The rule is reported verbatim by ErrJSONRequiresHeadless.
func ResolveMode(o Options, interactiveTTY bool) (Mode, error) {
	wizardEligible := interactiveTTY && !o.NonInteractive && !o.DryRun && !o.OneShot()
	if o.JSON && wizardEligible {
		return "", ErrJSONRequiresHeadless
	}
	if wizardEligible && !o.JSON {
		return ModeWizard, nil
	}
	if o.NonInteractive || o.DryRun || o.JSON || o.OneShot() {
		return ModeHeadless, nil
	}
	return ModePrompted, nil
}
