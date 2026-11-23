package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	linuxBin  = "/usr/lib/postgresql/18/bin"
	darwinBin = "/opt/homebrew/opt/postgresql@18/bin"
)

// apiOrbit declares one database; its role defaults to the database name.
const apiOrbit = "version: 1\nservices:\n  postgres:\n    databases:\n      - name: app_api\n"

// apiMailOrbit is apiOrbit plus mail.
const apiMailOrbit = apiOrbit + "  mail: true\n"

// fakeFS reports the Postgres bin directories as present only when listed. Every other path
// goes to the real filesystem, which the tests confine to their temporary directories.
type fakeFS struct {
	engine.OSFS
	present []string
}

func (f fakeFS) Stat(path string) (fs.FileInfo, error) {
	if path == linuxBin || path == darwinBin {
		if slices.Contains(f.present, path) {
			return nil, nil
		}
		return nil, fs.ErrNotExist
	}
	return f.OSFS.Stat(path)
}

// simCall is one command the sim received.
type simCall struct {
	name string
	args []string
}

// sim is a stateful stand-in for initdb, pg_ctl and psql. It writes the cluster files, tracks
// whether the server runs, and executes the SQL statements that EnsureDatabase generates.
type sim struct {
	t       *testing.T
	dataDir string
	running bool
	roles   map[string]bool
	dbs     map[string]string
	calls   []simCall
	stmts   []string
}

var (
	queryRe    = regexp.MustCompile(`SELECT 1 FROM (pg_roles|pg_database) WHERE (rolname|datname)='([^']*)'`)
	createRole = regexp.MustCompile(`^CREATE ROLE "([^"]+)" LOGIN PASSWORD '[^']*';$`)
	alterRole  = regexp.MustCompile(`^ALTER ROLE "([^"]+)" WITH LOGIN PASSWORD '[^']*';$`)
	createDB   = regexp.MustCompile(`^CREATE DATABASE "([^"]+)" OWNER "([^"]+)";$`)
)

func newSim(t *testing.T, dataDir string) *sim {
	return &sim{t: t, dataDir: dataDir, roles: map[string]bool{}, dbs: map[string]string{}}
}

// seed records an existing role and database, as left by an earlier run.
func (s *sim) seed(role, db string) {
	s.roles[role] = true
	s.dbs[db] = role
}

// count returns how many executed statements start with prefix.
func (s *sim) count(prefix string) int {
	n := 0
	for _, st := range s.stmts {
		if strings.HasPrefix(st, prefix) {
			n++
		}
	}
	return n
}

func (s *sim) Run(_ context.Context, _ string, name string, args ...string) (runner.Result, error) {
	s.calls = append(s.calls, simCall{name: name, args: append([]string(nil), args...)})
	switch filepath.Base(name) {
	case "pg_ctl":
		return s.pgCtl(args)
	case "initdb":
		return s.initdb(args)
	case "psql":
		return s.psql(args)
	}
	return runner.Result{}, fmt.Errorf("sim: unexpected command %s", name)
}

func (s *sim) pgCtl(args []string) (runner.Result, error) {
	switch {
	case slices.Contains(args, "status"):
		if s.running {
			return runner.Result{}, nil
		}
		return runner.Result{ExitCode: 3}, errors.New("pg_ctl: no server running")
	case slices.Contains(args, "start"):
		s.running = true
	case slices.Contains(args, "stop"):
		if !s.running {
			return runner.Result{ExitCode: 3}, errors.New("pg_ctl: no server running")
		}
		s.running = false
	}
	return runner.Result{}, nil
}

func (s *sim) initdb(args []string) (runner.Result, error) {
	dir := argValue(args, "-D")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return runner.Result{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("18\n"), 0o600); err != nil {
		return runner.Result{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "postgresql.conf"), []byte("# default config\n"), 0o600); err != nil {
		return runner.Result{}, err
	}
	return runner.Result{}, nil
}

func (s *sim) psql(args []string) (runner.Result, error) {
	if !s.running {
		return runner.Result{ExitCode: 2}, errors.New("psql: connection refused")
	}
	if i := slices.Index(args, "-tAc"); i >= 0 && i+1 < len(args) {
		m := queryRe.FindStringSubmatch(args[i+1])
		if m == nil {
			return runner.Result{ExitCode: 1}, fmt.Errorf("sim: unexpected query %q", args[i+1])
		}
		found := s.roles[m[3]]
		if m[1] == "pg_database" {
			_, found = s.dbs[m[3]]
		}
		if found {
			return runner.Result{Stdout: "1\n"}, nil
		}
		return runner.Result{}, nil
	}
	if i := slices.Index(args, "-f"); i >= 0 && i+1 < len(args) {
		return s.runSQL(args[i+1])
	}
	return runner.Result{ExitCode: 1}, errors.New("sim: psql without -f or -tAc")
}

func (s *sim) runSQL(path string) (runner.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return runner.Result{ExitCode: 1}, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		switch {
		case createRole.MatchString(line):
			name := createRole.FindStringSubmatch(line)[1]
			if s.roles[name] {
				return runner.Result{ExitCode: 1}, fmt.Errorf("role %q already exists", name)
			}
			s.roles[name] = true
			s.stmts = append(s.stmts, "CREATE ROLE "+name)
		case alterRole.MatchString(line):
			name := alterRole.FindStringSubmatch(line)[1]
			if !s.roles[name] {
				return runner.Result{ExitCode: 1}, fmt.Errorf("role %q does not exist", name)
			}
			s.stmts = append(s.stmts, "ALTER ROLE "+name)
		case createDB.MatchString(line):
			m := createDB.FindStringSubmatch(line)
			if !s.roles[m[2]] {
				return runner.Result{ExitCode: 1}, fmt.Errorf("owner %q does not exist", m[2])
			}
			if _, ok := s.dbs[m[1]]; ok {
				return runner.Result{ExitCode: 1}, fmt.Errorf("database %q already exists", m[1])
			}
			s.dbs[m[1]] = m[2]
			s.stmts = append(s.stmts, "CREATE DATABASE "+m[1])
		default:
			return runner.Result{ExitCode: 1}, fmt.Errorf("sim: unexpected statement")
		}
	}
	return runner.Result{}, nil
}

// argValue returns the element after flag, or "" when flag is absent.
func argValue(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

type killRecord struct {
	pid int
	sig syscall.Signal
}

// rig is one machine: a workspace with declared repos, the service directories under a
// temporary base, and scripted listeners, live pids and signals.
type rig struct {
	t         *testing.T
	root      string
	dataDir   string
	stateDir  string
	secrets   string
	present   []string
	listening map[string]bool
	alive     map[int]bool
	dialed    []string
	killed    []killRecord
}

func newRig(t *testing.T) *rig {
	t.Helper()
	base := t.TempDir()
	return &rig{
		t:         t,
		root:      filepath.Join(base, "ws"),
		dataDir:   filepath.Join(base, "share", "postgres"),
		stateDir:  filepath.Join(base, "state"),
		secrets:   filepath.Join(base, "config", "local.env"),
		present:   []string{linuxBin, darwinBin},
		listening: map[string]bool{},
		alive:     map[int]bool{},
	}
}

func (r *rig) dialFn(addr string, _ time.Duration) error {
	r.dialed = append(r.dialed, addr)
	if r.listening[addr] {
		return nil
	}
	return errors.New("connection refused")
}

func (r *rig) aliveFn(pid int) bool { return r.alive[pid] }

func (r *rig) killFn(pid int, sig syscall.Signal) error {
	r.killed = append(r.killed, killRecord{pid: pid, sig: sig})
	return nil
}

func (r *rig) deps() deps {
	return deps{
		dataDir:     func() (string, error) { return r.dataDir, nil },
		secretsPath: func() (string, error) { return r.secrets, nil },
		stateDir:    func() (string, error) { return r.stateDir, nil },
		user:        func() (string, error) { return "tester", nil },
		dial:        r.dialFn,
		alive:       r.aliveFn,
		kill:        r.killFn,
	}
}

func (r *rig) part() part { return part{deps: r.deps()} }

func (r *rig) postgres(run runner.Runner) *postgres {
	return &postgres{
		run:         run,
		fs:          engine.OSFS{},
		bin:         linuxBin,
		dataDir:     r.dataDir,
		secretsPath: r.secrets,
		user:        "tester",
		dial:        r.dialFn,
	}
}

func (r *rig) mailpit(run runner.Runner) *mailpit {
	return &mailpit{
		run:      run,
		fs:       engine.OSFS{},
		stateDir: r.stateDir,
		dial:     r.dialFn,
		alive:    r.aliveFn,
		kill:     r.killFn,
	}
}

// mkCluster creates the data directory, as an initialised cluster would have it.
func (r *rig) mkCluster() {
	r.t.Helper()
	if err := os.MkdirAll(r.dataDir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dataDir, "PG_VERSION"), []byte("18\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// addRepo writes orbit.yaml into the repo at path under the workspace root.
func (r *rig) addRepo(path, orbit string) {
	r.t.Helper()
	dir := filepath.Join(r.root, path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orbit.yaml"), []byte(orbit), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// state builds the engine state for an "app" project that selects repos.
func (r *rig) state(platform, services string, run runner.Runner, repos ...string) *engine.State {
	r.t.Helper()
	var configs []manifest.RepoConfig
	for _, path := range repos {
		name := filepath.Base(path)
		configs = append(configs, manifest.RepoConfig{
			Name:          name,
			Path:          path,
			RemoteURL:     "git@github.com:manovaspace/" + name + ".git",
			DefaultBranch: "main",
		})
	}
	return &engine.State{
		Root:     r.root,
		Machine:  spec.Machine{Projects: []string{"app"}, Services: services, GitProtocol: "https"},
		Platform: platform,
		Manifest: &manifest.WorkspaceManifest{
			Groups:   map[string]manifest.GroupConfig{"all": {Repositories: configs}},
			Projects: map[string]manifest.ProjectConfig{"app": {Repos: repos}},
		},
		Run: run,
		FS:  fakeFS{present: r.present},
	}
}

func summaries(plan engine.Plan) []string {
	out := make([]string, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		out = append(out, a.Summary)
	}
	return out
}

func readSecrets(t *testing.T, path string) map[string]string {
	t.Helper()
	got, err := LocalSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
