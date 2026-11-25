package pkgmgr

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

// apt manages Debian packages on Ubuntu.
type apt struct {
	common
}

var _ Manager = (*apt)(nil)

func (a *apt) Missing(ctx context.Context, names []string) ([]string, error) {
	var out []string
	for _, name := range names {
		p, err := a.recipe(name)
		if err != nil {
			return nil, err
		}
		var installed bool
		switch {
		case p.Apt != "":
			installed, err = a.packageInstalled(ctx, p.Apt)
			if err != nil {
				return nil, err
			}
		case p.Script != "":
			installed = a.scriptInstalled(p)
		default:
			continue
		}
		if !installed {
			out = append(out, name)
		}
	}
	return out, nil
}

// packageInstalled reports a package as installed when dpkg reports the install state ok.
func (a *apt) packageInstalled(ctx context.Context, pkg string) (bool, error) {
	out, err := a.queryOutput(ctx, "dpkg-query", "-W", "-f=${Status}", pkg)
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "install ok installed"), nil
}

// Install plans, in order: a prerequisites action when a repository recipe is planned and curl
// or ca-certificates is not installed (curl then leaves the main batch), one repository action
// per distinct apt_repo not yet configured (sorted by recipe name), the postgres main-cluster
// setting when postgresql-18 is in the batch, one apt-get update and install action, then one
// action per script package.
func (a *apt) Install(names []string) []engine.Action {
	var pkgs []string
	repos := make(map[string]bool)
	var scripts []engine.Action
	for _, name := range names {
		p, ok := a.pkgs[name]
		if !ok {
			continue
		}
		switch {
		case p.Apt != "":
			pkgs = append(pkgs, p.Apt)
			if p.AptRepo != "" {
				repos[p.AptRepo] = true
			}
		case p.Script != "":
			scripts = append(scripts, a.scriptAction(name, p))
		}
	}

	var repoActions []engine.Action
	for _, repo := range slices.Sorted(maps.Keys(repos)) {
		if action, ok := a.repoAction(repo); ok {
			repoActions = append(repoActions, action)
		}
	}

	var actions []engine.Action
	if len(repoActions) > 0 && a.bootstrapNeeded() {
		actions = append(actions, a.bootstrapAction())
		pkgs = slices.DeleteFunc(pkgs, func(pkg string) bool { return pkg == "curl" })
	}
	actions = append(actions, repoActions...)
	if slices.Contains(pkgs, pgAptPackage) && !a.clusterConfigured() {
		actions = append(actions, a.clusterAction())
	}
	if len(pkgs) > 0 {
		actions = append(actions, a.installAction(pkgs))
	}
	return append(actions, scripts...)
}

// bootstrapNeeded reports whether curl or ca-certificates is missing, which the repository
// recipes need. A query that fails is treated as missing: reinstalling is harmless.
func (a *apt) bootstrapNeeded() bool {
	for _, pkg := range []string{"ca-certificates", "curl"} {
		if ok, err := a.packageInstalled(context.Background(), pkg); err != nil || !ok {
			return true
		}
	}
	return false
}

const aptInstallPrefix = `apt-get update -o DPkg::Lock::Timeout=120 && DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install -y -o DPkg::Lock::Timeout=120 -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" `

// bootstrapAction installs the tools the repository recipes depend on.
func (a *apt) bootstrapAction() engine.Action {
	script := aptInstallPrefix + "ca-certificates curl"
	return engine.Action{
		Subject: "apt-prerequisites",
		Summary: "Install apt prerequisites",
		Sudo:    true,
		Do: func(ctx context.Context) error {
			return a.tty.RunInteractive(ctx, "", nil, "sudo", "sh", "-c", script)
		},
	}
}

// repoAction adds the apt repository recipe name unless its list file already exists.
func (a *apt) repoAction(name string) (engine.Action, bool) {
	r, ok := aptRepos[name]
	if !ok {
		return engine.Action{}, false
	}
	if _, err := a.fs.Stat(r.List); err == nil {
		return engine.Action{}, false
	}
	return engine.Action{
		Subject: name,
		Summary: "add apt repository " + name,
		Sudo:    true,
		Do: func(ctx context.Context) error {
			return a.tty.RunInteractive(ctx, "", nil, "sudo", "sh", "-c", r.script())
		},
	}, true
}

// clusterConfigured reports whether createcluster.conf already disables the main cluster.
func (a *apt) clusterConfigured() bool {
	data, err := a.fs.ReadFile(clusterConfig)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "create_main_cluster = false" {
			return true
		}
	}
	return false
}

// clusterAction stops postgres-common from creating a main cluster at install time.
func (a *apt) clusterAction() engine.Action {
	script := "install -d /etc/postgresql-common && echo 'create_main_cluster = false' >> " + clusterConfig
	return engine.Action{
		Subject: pgAptPackage,
		Summary: "disable the automatic main cluster for " + pgAptPackage,
		Sudo:    true,
		Do: func(ctx context.Context) error {
			return a.tty.RunInteractive(ctx, "", nil, "sudo", "sh", "-c", script)
		},
	}
}

// installAction refreshes the package index and installs pkgs.
func (a *apt) installAction(pkgs []string) engine.Action {
	list := strings.Join(pkgs, " ")
	script := aptInstallPrefix + list
	return engine.Action{
		Subject: "apt",
		Summary: "apt-get update, then install " + list,
		Sudo:    true,
		Do: func(ctx context.Context) error {
			return a.tty.RunInteractive(ctx, "", nil, "sudo", "sh", "-c", script)
		},
	}
}
