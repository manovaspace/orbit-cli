// Package pkgmgr plans OS package installs for setup parts: Homebrew formulae on macOS, apt
// packages on Ubuntu, and script installers on both. It only plans; every Action runs its
// command through the state's Tty, so a sudo prompt reaches the user.
package pkgmgr

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// Manager plans installs of logical package names from spec.Packages for one platform.
type Manager interface {
	// Missing returns the names that are not installed, in input order. A name with no recipe
	// for this platform is skipped, never reported missing. An unknown name is an error.
	Missing(ctx context.Context, names []string) ([]string, error)
	// Install plans the actions that install names, normally the result of Missing. Names
	// without a recipe for this platform are ignored.
	Install(names []string) []engine.Action
}

// LookPath resolves a command name on PATH. exec.LookPath is the production value.
type LookPath func(file string) (string, error)

// For returns the manager for s.Platform, looking commands up with exec.LookPath.
func For(s *engine.State) (Manager, error) {
	return ForLookPath(s, exec.LookPath)
}

// ForLookPath is For with an injected command lookup, used for script package detection.
func ForLookPath(s *engine.State, lookPath LookPath) (Manager, error) {
	pkgs, err := spec.Packages()
	if err != nil {
		return nil, err
	}
	c := common{run: s.Run, tty: s.Tty, fs: s.FS, lookPath: lookPath, pkgs: pkgs}
	switch s.Platform {
	case "darwin":
		return &brew{c}, nil
	case "linux":
		return &apt{c}, nil
	}
	return nil, clierr.Unverified(fmt.Sprintf("no package manager for platform %q", s.Platform))
}

// common holds what every manager needs from the state and the package recipes.
type common struct {
	run      runner.Runner
	tty      runner.Interactive
	fs       engine.FS
	lookPath LookPath
	pkgs     map[string]spec.Package
}

// recipe returns the recipe for name, or an error for a name that has none.
func (c common) recipe(name string) (spec.Package, error) {
	p, ok := c.pkgs[name]
	if !ok {
		return spec.Package{}, clierr.Failed(fmt.Sprintf("no package recipe named %q", name))
	}
	return p, nil
}

// queryOutput runs a read-only query and returns its stdout. A failing command (non-zero exit
// or missing tool) yields empty output, which callers read as not installed. Only a cancelled
// context is an error.
func (c common) queryOutput(ctx context.Context, name string, args ...string) (string, error) {
	res, err := c.run.Run(ctx, "", name, args...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		return "", nil
	}
	return res.Stdout, nil
}

// scriptInstalled reports whether a script package's Detect command is on PATH.
func (c common) scriptInstalled(p spec.Package) bool {
	_, err := c.lookPath(p.Detect)
	return err == nil
}

// scriptAction installs a script package without sudo: curl the installer and pipe it to bash.
func (c common) scriptAction(name string, p spec.Package) engine.Action {
	return engine.Action{
		Subject: name,
		Summary: fmt.Sprintf("install %s from %s", name, p.Script),
		Do: func(ctx context.Context) error {
			return c.tty.RunInteractive(ctx, "", nil, "bash", "-c", "curl -fsSL "+p.Script+" | bash")
		},
	}
}

// brew manages Homebrew formulae on macOS.
type brew struct {
	common
}

var _ Manager = (*brew)(nil)

func (b *brew) Missing(ctx context.Context, names []string) ([]string, error) {
	var out []string
	for _, name := range names {
		p, err := b.recipe(name)
		if err != nil {
			return nil, err
		}
		var installed bool
		switch {
		case p.Brew != "":
			installed, err = b.formulaInstalled(ctx, p.Brew)
			if err != nil {
				return nil, err
			}
		case p.Script != "":
			installed = b.scriptInstalled(p)
		default:
			continue
		}
		if !installed {
			out = append(out, name)
		}
	}
	return out, nil
}

// formulaInstalled reports a formula as installed when brew lists a version for it.
func (b *brew) formulaInstalled(ctx context.Context, formula string) (bool, error) {
	out, err := b.queryOutput(ctx, "brew", "list", "--versions", formula)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (b *brew) Install(names []string) []engine.Action {
	var formulae []string
	var scripts []engine.Action
	for _, name := range names {
		p, ok := b.pkgs[name]
		if !ok {
			continue
		}
		switch {
		case p.Brew != "":
			formulae = append(formulae, p.Brew)
		case p.Script != "":
			scripts = append(scripts, b.scriptAction(name, p))
		}
	}
	var actions []engine.Action
	if len(formulae) > 0 {
		args := append([]string{"install"}, formulae...)
		actions = append(actions, engine.Action{
			Subject: "brew",
			Summary: "brew " + strings.Join(args, " "),
			Do: func(ctx context.Context) error {
				return b.tty.RunInteractive(ctx, "", nil, "brew", args...)
			},
		})
	}
	return append(actions, scripts...)
}
