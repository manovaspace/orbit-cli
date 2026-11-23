// Package system brings a machine to the base tools every workspace needs: Homebrew on
// macOS, then git, gh and curl through the platform package manager.
package system

import (
	"context"
	"os"
	"os/exec"
	"runtime"

	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/pkgmgr"
)

const partName = "system"

// basePackages are the logical names from packages.yaml the system part installs.
var basePackages = []string{"git", "gh", "curl"}

// homebrewInstall is the official installer, run as a shell command line on the target.
const homebrewInstall = `$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)`

// brewPaths are where the Homebrew installer puts brew on macOS.
var brewPaths = []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"}

type part struct {
	lookPath pkgmgr.LookPath
}

// New returns the system part, looking commands up on PATH.
func New() engine.Part {
	return part{lookPath: exec.LookPath}
}

func (p part) Name() string    { return partName }
func (p part) Needs() []string { return nil }

// Plan adds the Homebrew install on macOS when brew is absent, then the package manager actions
// for the base packages that are missing. The Homebrew action runs first, so the brew install
// that follows finds brew on PATH.
func (p part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	var plan engine.Plan
	if s.Platform == "darwin" && !p.brewPresent(s) {
		plan.Actions = append(plan.Actions, homebrewAction(s))
	}
	mgr, err := pkgmgr.ForLookPath(s, p.lookPath)
	if err != nil {
		return engine.Plan{}, err
	}
	missing, err := mgr.Missing(ctx, basePackages)
	if err != nil {
		return engine.Plan{}, err
	}
	for _, action := range mgr.Install(missing) {
		action.Part = partName
		plan.Actions = append(plan.Actions, action)
	}
	return plan, nil
}

// brewPresent reports whether brew is on PATH or at one of the installer's fixed locations.
func (p part) brewPresent(s *engine.State) bool {
	if _, err := p.lookPath("brew"); err == nil {
		return true
	}
	for _, path := range brewPaths {
		if _, err := s.FS.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// homebrewAction runs the official installer non-interactively, then puts the prefix that
// matches this machine's architecture first on PATH for the rest of the run.
func homebrewAction(s *engine.State) engine.Action {
	tty := s.Tty
	prefix := "/usr/local/bin"
	if runtime.GOARCH == "arm64" {
		prefix = "/opt/homebrew/bin"
	}
	return engine.Action{
		Part:    partName,
		Subject: "homebrew",
		Summary: "Install Homebrew",
		Sudo:    true,
		Do: func(ctx context.Context) error {
			if err := tty.RunInteractive(ctx, "", []string{"NONINTERACTIVE=1"}, "/bin/bash", "-c", homebrewInstall); err != nil {
				return err
			}
			return os.Setenv("PATH", prefix+string(os.PathListSeparator)+os.Getenv("PATH"))
		},
	}
}
