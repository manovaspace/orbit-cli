// Package system brings a machine to the base tools every workspace needs: Homebrew on
// macOS, then git, gh and curl through the platform package manager.
package system

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/pkgmgr"
)

const partName = "system"

// basePackages are the logical names from packages.yaml the system part installs.
var basePackages = []string{"git", "gh", "curl", "unzip"}

// homebrewInstall is the shell command line that runs the official installer. Run by bash -c
// as an argument, so the outer bash expands the $(curl ...) substitution and the inner bash
// receives the installer script.
const homebrewInstall = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// brewPaths are where the Homebrew installer puts brew on macOS.
var brewPaths = []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"}

type part struct {
	lookPath pkgmgr.LookPath
	setenv   func(key, value string) error
}

// New returns the system part, looking commands up on PATH and setting PATH in this process.
func New() engine.Part {
	return part{lookPath: exec.LookPath, setenv: os.Setenv}
}

func (p part) Name() string    { return partName }
func (p part) Needs() []string { return nil }

// Plan works out the Homebrew state on macOS, then plans the package manager actions for the
// missing base packages. When brew is on neither PATH nor a fixed path, the Homebrew action is
// planned and every base package is planned without querying brew, because brew does not exist
// yet. When brew exists only at a fixed path, that directory is put first on PATH.
func (p part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	var plan engine.Plan
	mgr, err := pkgmgr.ForLookPath(s, p.lookPath)
	if err != nil {
		return engine.Plan{}, err
	}

	missing := basePackages
	if s.Platform == "darwin" {
		if _, lookErr := p.lookPath("brew"); lookErr != nil {
			dir, found := brewDir(s)
			if !found {
				plan.Actions = append(plan.Actions, p.homebrewAction(s))
				return p.addInstalls(plan, mgr, missing), nil
			}
			if err := p.prependPath(dir); err != nil {
				return engine.Plan{}, err
			}
		}
	}

	missing, err = mgr.Missing(ctx, basePackages)
	if err != nil {
		return engine.Plan{}, err
	}
	return p.addInstalls(plan, mgr, missing), nil
}

// addInstalls appends the install actions for missing, stamped with this part's name.
func (p part) addInstalls(plan engine.Plan, mgr pkgmgr.Manager, missing []string) engine.Plan {
	for _, action := range mgr.Install(missing) {
		action.Part = partName
		plan.Actions = append(plan.Actions, action)
	}
	return plan
}

// brewDir returns the directory of the first fixed-path brew that exists.
func brewDir(s *engine.State) (string, bool) {
	for _, path := range brewPaths {
		if _, err := s.FS.Stat(path); err == nil {
			return filepath.Dir(path), true
		}
	}
	return "", false
}

// prependPath puts dir first on PATH unless it is already there.
func (p part) prependPath(dir string) error {
	current := os.Getenv("PATH")
	for _, entry := range filepath.SplitList(current) {
		if entry == dir {
			return nil
		}
	}
	return p.setenv("PATH", dir+string(os.PathListSeparator)+current)
}

// homebrewAction runs the official installer non-interactively, then puts the prefix that
// matches this machine's architecture first on PATH for the rest of the run.
func (p part) homebrewAction(s *engine.State) engine.Action {
	prefix := "/usr/local/bin"
	if runtime.GOARCH == "arm64" {
		prefix = "/opt/homebrew/bin"
	}
	tty := s.Tty
	return engine.Action{
		Part:    partName,
		Subject: "homebrew",
		Summary: "Install Homebrew",
		Sudo:    true,
		Do: func(ctx context.Context) error {
			if err := tty.RunInteractive(ctx, "", []string{"NONINTERACTIVE=1"}, "/bin/bash", "-c", homebrewInstall); err != nil {
				return err
			}
			return p.prependPath(prefix)
		},
	}
}
