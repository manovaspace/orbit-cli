package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// HandbookRepo is the GitHub repository of the staff handbook.
const HandbookRepo = "manovaspace/handbook"

// HandbookPath is the workspace path of the handbook clone.
const HandbookPath = "handbook"

// RepoSpec is a repository's orbit.yaml.
type RepoSpec struct {
	Version    int                `yaml:"version" json:"version"`
	Toolchains []string           `yaml:"toolchains" json:"toolchains"`
	Services   ServicesSpec       `yaml:"services" json:"services"`
	Prepare    []string           `yaml:"prepare" json:"prepare"`
	Migrate    []string           `yaml:"migrate" json:"migrate"`
	Seed       []string           `yaml:"seed" json:"seed"`
	Env        []EnvOutput        `yaml:"env" json:"env"`
	Run        map[string]RunSpec `yaml:"run" json:"run"`
}

// ServicesSpec lists the local services a repo needs.
type ServicesSpec struct {
	Postgres *PostgresSpec `yaml:"postgres" json:"postgres"`
	Mail     bool          `yaml:"mail" json:"mail"`
}

// PostgresSpec lists the databases a repo needs on the shared Postgres.
type PostgresSpec struct {
	Databases []DatabaseSpec `yaml:"databases" json:"databases"`
}

// DatabaseSpec names one database and its role. Role defaults to Name.
type DatabaseSpec struct {
	Name string `yaml:"name" json:"name"`
	Role string `yaml:"role" json:"role"`
}

// EnvOutput renders an env file from a schema into a relative path.
type EnvOutput struct {
	Schema string `yaml:"schema" json:"schema"`
	Write  string `yaml:"write" json:"write"`
}

// RunSpec is a named dev process.
type RunSpec struct {
	Cmd  string `yaml:"cmd" json:"cmd"`
	Port string `yaml:"port" json:"port"`
}

var (
	nameRe         = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	toolchainNames = map[string]bool{"go": true, "bun": true, "node": true}
)

// LoadRepoSpec reads repoDir/orbit.yaml. The bool reports whether the file existed.
func LoadRepoSpec(repoDir string) (RepoSpec, bool, error) {
	path := filepath.Join(repoDir, "orbit.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return RepoSpec{}, false, nil
	}
	if err != nil {
		return RepoSpec{}, true, clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot read %s", path)))
	}
	var rs RepoSpec
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return RepoSpec{}, true, clierr.Wrap(err, clierr.Usage(fmt.Sprintf("%s is malformed", path)))
	}
	if err := rs.validate(path); err != nil {
		return RepoSpec{}, true, err
	}
	return rs, true, nil
}

func (rs *RepoSpec) validate(path string) error {
	if rs.Version != 1 {
		return clierr.Usage(fmt.Sprintf("%s: version must be 1, got %d", path, rs.Version))
	}
	for _, tc := range rs.Toolchains {
		if !toolchainNames[tc] {
			return clierr.Usage(fmt.Sprintf("%s: toolchain %q is invalid: want go, bun or node", path, tc))
		}
	}
	if rs.Services.Postgres != nil {
		for i := range rs.Services.Postgres.Databases {
			db := &rs.Services.Postgres.Databases[i]
			if !nameRe.MatchString(db.Name) {
				return clierr.Usage(fmt.Sprintf("%s: database name %q must match %s", path, db.Name, nameRe))
			}
			if db.Role == "" {
				db.Role = db.Name
			}
			if !nameRe.MatchString(db.Role) {
				return clierr.Usage(fmt.Sprintf("%s: role name %q must match %s", path, db.Role, nameRe))
			}
		}
	}
	for _, env := range rs.Env {
		if err := checkRelative(env.Write); err != nil {
			return clierr.Usage(fmt.Sprintf("%s: env write %q: %v", path, env.Write, err))
		}
	}
	return nil
}

// checkRelative rejects empty, absolute and ".."-containing paths.
func checkRelative(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("path must be relative")
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return fmt.Errorf("path must not contain ..")
		}
	}
	return nil
}
