package pkgmgr

import "fmt"

const (
	pgAptPackage  = "postgresql-18"
	clusterConfig = "/etc/postgresql-common/createcluster.conf"
)

// aptRepo is the recipe for one apt source named by spec.Package.AptRepo. Its script fetches
// the signing key into Keyring and writes Line to List, all as root.
type aptRepo struct {
	Key     string // URL of the signing key
	Keyring string // path the key is saved to
	Line    string // deb source line; $(...) is expanded by the shell on the target
	List    string // apt source file; its presence means the repo is configured
}

// aptRepos holds every repository recipe the packages.yaml apt_repo values refer to.
var aptRepos = map[string]aptRepo{
	"github-cli": {
		Key:     "https://cli.github.com/packages/githubcli-archive-keyring.gpg",
		Keyring: "/etc/apt/keyrings/githubcli-archive-keyring.gpg",
		Line:    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main",
		List:    "/etc/apt/sources.list.d/github-cli.list",
	},
	"caddy-stable": {
		Key:     "https://dl.cloudsmith.io/public/caddy/stable/gpg.key",
		Keyring: "/etc/apt/keyrings/caddy-stable.asc",
		Line:    "deb [signed-by=/etc/apt/keyrings/caddy-stable.asc] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main",
		List:    "/etc/apt/sources.list.d/caddy-stable.list",
	},
	"pgdg": {
		Key:     "https://www.postgresql.org/media/keys/ACCC4CF8.asc",
		Keyring: "/etc/apt/keyrings/pgdg.asc",
		Line:    "deb [signed-by=/etc/apt/keyrings/pgdg.asc] https://apt.postgresql.org/pub/repos/apt $(. /etc/os-release && echo $VERSION_CODENAME)-pgdg main",
		List:    "/etc/apt/sources.list.d/pgdg.list",
	},
	"nodesource-24": {
		Key:     "https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key",
		Keyring: "/etc/apt/keyrings/nodesource.asc",
		Line:    "deb [signed-by=/etc/apt/keyrings/nodesource.asc] https://deb.nodesource.com/node_24.x nodistro main",
		List:    "/etc/apt/sources.list.d/nodesource.list",
	},
}

// script is the root shell script that adds the repository.
func (r aptRepo) script() string {
	return fmt.Sprintf(
		"install -d -m 0755 /etc/apt/keyrings && curl -fsSL %s -o %s && chmod a+r %s && echo \"%s\" > %s",
		r.Key, r.Keyring, r.Keyring, r.Line, r.List,
	)
}
