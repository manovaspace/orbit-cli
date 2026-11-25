package prepare

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// orbitFull declares every command kind, one database and the default env output. Dummy values only.
const orbitFull = `version: 1
prepare:
  - bun install
migrate:
  - bun run db:migrate
seed:
  - bun run db:seed
services:
  postgres:
    databases:
      - name: app_api
env:
  - schema: .env.schema.yaml
    write: .env
`

// fixture is a real temporary workspace with one repository, clients/alpha, and a State wired to a
// runnertest.Fake for both Run and RunEnv. The prepare state file lives in the workspace.
type fixture struct {
	repo  string
	state string
	s     *engine.State
	fake  *runnertest.Fake
	git   *gitFake
	p     part
}

// gitFake answers git rev-parse HEAD with head, which a test may change between plans. Every other
// command goes to the Fake.
type gitFake struct {
	*runnertest.Fake
	head string
}

func (g *gitFake) Run(ctx context.Context, dir, name string, args ...string) (runner.Result, error) {
	if name == "git" {
		return runner.Result{Stdout: g.head + "\n"}, nil
	}
	return g.Fake.Run(ctx, dir, name, args...)
}

// newFixture writes orbit as clients/alpha/orbit.yaml when orbit is not empty.
func newFixture(t *testing.T, services, orbit string) fixture {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "clients", "alpha")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if orbit != "" {
		write(t, filepath.Join(repo, "orbit.yaml"), orbit)
	}
	state := filepath.Join(dir, "state", "orbit", "prepare.json")
	fake := &runnertest.Fake{}
	git := &gitFake{Fake: fake, head: "head1"}
	s := &engine.State{
		Root:     dir,
		Machine:  spec.Machine{Projects: []string{"app"}, Services: services},
		Manifest: testManifest(),
		Run:      git,
		Env:      fake,
		FS:       engine.OSFS{},
	}
	p := part{statePath: func() (string, error) { return state, nil }}
	return fixture{repo: repo, state: state, s: s, fake: fake, git: git, p: p}
}

// testManifest selects one repository, clients/alpha, through project app.
func testManifest() *manifest.WorkspaceManifest {
	return &manifest.WorkspaceManifest{
		Groups: map[string]manifest.GroupConfig{
			"clients": {Repositories: []manifest.RepoConfig{{
				Name: "alpha", Path: "clients/alpha", RemoteURL: "git@github.com:manovaspace/alpha.git", DefaultBranch: "main",
			}}},
		},
		Projects: map[string]manifest.ProjectConfig{
			"app": {Repos: []string{"clients/alpha"}},
		},
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// head sets the git HEAD answer of the repository.
func (f fixture) head(sha string) {
	f.git.head = sha
}

// ok scripts a successful sh -c command.
func (f fixture) ok(command string) {
	f.fake.On("sh -c "+command, runner.Result{}, nil)
}

func (f fixture) plan(t *testing.T) engine.Plan {
	t.Helper()
	got, err := f.p.Plan(context.Background(), f.s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return got
}

// apply runs every action of got in order and fails the test on the first error.
func apply(t *testing.T, got engine.Plan) {
	t.Helper()
	for _, a := range got.Actions {
		if err := a.Do(context.Background()); err != nil {
			t.Fatalf("action %q: %v", a.Summary, err)
		}
	}
}

func summaries(got engine.Plan) []string {
	out := make([]string, 0, len(got.Actions))
	for _, a := range got.Actions {
		out = append(out, a.Summary)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// shCalls returns the sh -c command lines the fake received, in order.
func shCalls(f *runnertest.Fake) []runnertest.Call {
	var out []runnertest.Call
	for _, c := range f.Calls() {
		if c.Name == "sh" {
			out = append(out, c)
		}
	}
	return out
}

func TestPrepareRunsOnLockfileChangeOnly(t *testing.T) {
	f := newFixture(t, "native", orbitFull)
	f.head("head1")
	f.ok("bun install")
	f.ok("bun run db:migrate")
	write(t, filepath.Join(f.repo, "bun.lock"), "lock-v1")
	write(t, filepath.Join(f.repo, "node_modules", "dep", "bun.lock"), "ignored")

	first := f.plan(t)
	if !contains(summaries(first), "clients/alpha: bun install") {
		t.Fatalf("first run summaries = %v, want prepare", summaries(first))
	}
	apply(t, first)
	if _, err := os.Stat(f.state); err != nil {
		t.Fatalf("state file not written: %v", err)
	}

	// Same lockfile, same HEAD, no new database: nothing is planned at all.
	second := f.plan(t)
	if len(second.Actions) != 0 {
		t.Fatalf("second run summaries = %v, want none", summaries(second))
	}

	// A change under node_modules does not count; a change to the lockfile does.
	write(t, filepath.Join(f.repo, "node_modules", "dep", "bun.lock"), "still ignored")
	if got := f.plan(t); len(got.Actions) != 0 {
		t.Fatalf("node_modules change planned %v, want none", summaries(got))
	}
	write(t, filepath.Join(f.repo, "bun.lock"), "lock-v2")
	third := f.plan(t)
	if !contains(summaries(third), "clients/alpha: bun install") {
		t.Fatalf("lockfile change summaries = %v, want prepare", summaries(third))
	}
	if contains(summaries(third), "clients/alpha: bun run db:migrate") {
		t.Fatalf("lockfile change also planned migrate with an unchanged HEAD: %v", summaries(third))
	}
}

func TestMigrateEveryRunSeedOnlyOnNewDb(t *testing.T) {
	// Ruling 1: migrate runs when HEAD moves or a database is new, not on every apply; seed runs only
	// when a database is new.
	f := newFixture(t, "native", orbitFull)
	f.head("head1")
	f.ok("bun install")
	f.ok("bun run db:migrate")
	f.ok("bun run db:seed")

	first := f.plan(t)
	if !contains(summaries(first), "clients/alpha: bun run db:migrate") {
		t.Fatalf("first run summaries = %v, want migrate", summaries(first))
	}
	if contains(summaries(first), "clients/alpha: bun run db:seed") {
		t.Fatalf("seed planned without a new database: %v", summaries(first))
	}
	apply(t, first)

	again := f.plan(t)
	if len(again.Actions) != 0 {
		t.Fatalf("same HEAD summaries = %v, want none", summaries(again))
	}

	f.head("head2")
	moved := f.plan(t)
	if !contains(summaries(moved), "clients/alpha: bun run db:migrate") {
		t.Fatalf("moved HEAD summaries = %v, want migrate", summaries(moved))
	}
	apply(t, moved)

	f.s.NewDatabases = map[string]bool{"app_api": true}
	fresh := f.plan(t)
	got := summaries(fresh)
	migrate := indexOf(got, "clients/alpha: bun run db:migrate")
	seed := indexOf(got, "clients/alpha: bun run db:seed")
	if migrate < 0 || seed < 0 || migrate > seed {
		t.Fatalf("new database summaries = %v, want migrate then seed", got)
	}
	if contains(got, "clients/alpha: bun install") {
		t.Fatalf("new database planned prepare without a lockfile change: %v", got)
	}
}

func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

func TestCommandsGetEnvFromWrittenFile(t *testing.T) {
	orbit := `version: 1
prepare:
  - bun install
env:
  - schema: .env.schema.yaml
    write: .env
  - schema: .env.schema.yaml
    write: .env.local
`
	f := newFixture(t, "native", orbit)
	write(t, filepath.Join(f.repo, ".env"), "APP_TOKEN=dummy-first\nSHARED=one\n")
	write(t, filepath.Join(f.repo, ".env.local"), "APP_TOKEN=dummy-second\nLOCAL_ONLY=two\n")
	f.ok("bun install")

	apply(t, f.plan(t))

	calls := shCalls(f.fake)
	if len(calls) != 1 {
		t.Fatalf("sh calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.Dir != f.repo || strings.Join(c.Args, " ") != "-c bun install" {
		t.Fatalf("call = dir %q args %v, want repo dir and -c bun install", c.Dir, c.Args)
	}
	env := strings.Join(c.Env, "\n")
	for _, want := range []string{"APP_TOKEN=dummy-first", "SHARED=one", "LOCAL_ONLY=two"} {
		if !strings.Contains(env, want) {
			t.Fatalf("env %q lacks %s", env, want)
		}
	}
	if strings.Contains(env, "APP_TOKEN=dummy-second") {
		t.Fatal("second file won a duplicate key; first Env write must win")
	}
}

func TestExternalModeSkipsMigrations(t *testing.T) {
	// No git answer and no migrate/seed answers are scripted: any such call fails the test.
	f := newFixture(t, "external", orbitFull)
	f.ok("bun install")
	f.s.NewDatabases = map[string]bool{"app_api": true}

	got := f.plan(t)
	if !contains(summaries(got), "clients/alpha: bun install") {
		t.Fatalf("summaries = %v, want prepare still run", summaries(got))
	}
	if contains(summaries(got), "clients/alpha: bun run db:migrate") || contains(summaries(got), "clients/alpha: bun run db:seed") {
		t.Fatalf("external mode planned migrate or seed: %v", summaries(got))
	}
	want := "clients/alpha: migrations not run against shared databases (services: external)"
	var warned bool
	for _, f := range got.Findings {
		if f.Message == want && f.Severity == engine.SeverityWarn {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("findings = %+v, want warn %q", got.Findings, want)
	}
	apply(t, got)
	for _, c := range f.fake.Calls() {
		if c.Name != "sh" || strings.Join(c.Args, " ") != "-c bun install" {
			t.Fatalf("unexpected call %s %v", c.Name, c.Args)
		}
	}
}

func TestCorruptStateWarnsAndReruns(t *testing.T) {
	f := newFixture(t, "native", orbitFull)
	f.head("head1")
	f.ok("bun install")
	write(t, f.state, "{not json")

	got := f.plan(t)
	var warned bool
	for _, fd := range got.Findings {
		if fd.Message == "prepare state unreadable; rerunning prepare" && fd.Severity == engine.SeverityWarn {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("findings = %+v, want warn for unreadable state", got.Findings)
	}
	if !contains(summaries(got), "clients/alpha: bun install") {
		t.Fatalf("summaries = %v, want prepare rerun", summaries(got))
	}
}

func TestFailureReportsStderrTail(t *testing.T) {
	var lines []string
	for i := 1; i <= 25; i++ {
		lines = append(lines, fmt.Sprintf("line-%02d", i))
	}
	f := newFixture(t, "native", orbitFull)
	f.head("head1")
	f.fake.On("sh -c bun install", runner.Result{Stderr: strings.Join(lines, "\n") + "\n", ExitCode: 1},
		clierr.Tool("sh -c bun install exited with code 1"))

	got := f.plan(t)
	if len(got.Actions) == 0 {
		t.Fatal("no actions planned")
	}
	err := got.Actions[0].Do(context.Background())
	if clierr.Code(err) != clierr.CodeTool {
		t.Fatalf("error code = %q (%v), want tool", clierr.Code(err), err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "clients/alpha: bun install failed") {
		t.Fatalf("error = %q, want header", msg)
	}
	if !strings.Contains(msg, "line-25") || !strings.Contains(msg, "line-06") || strings.Contains(msg, "line-05") {
		t.Fatalf("error = %q, want last 20 stderr lines only", msg)
	}
	if _, statErr := os.Stat(f.state); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state written after failure: %v", statErr)
	}
}

func TestRepoWithoutOrbitYAMLContributesNothing(t *testing.T) {
	f := newFixture(t, "native", "")
	if got := f.plan(t); len(got.Actions) != 0 || len(got.Findings) != 0 {
		t.Fatalf("plan = %+v, want nothing", got)
	}
}

func TestSeedRetriedAfterFailure(t *testing.T) {
	f := newFixture(t, "native", orbitFull)
	f.head("head1")
	f.ok("bun install")
	f.ok("bun run db:migrate")
	f.fake.On("sh -c bun run db:seed", runner.Result{Stderr: "seed boom\n", ExitCode: 1}, clierr.Tool("seed exited 1"))
	f.s.NewDatabases = map[string]bool{"app_api": true}

	first := f.plan(t)
	var seedErr error
	for _, a := range first.Actions {
		if err := a.Do(context.Background()); err != nil {
			seedErr = err
			break
		}
	}
	if seedErr == nil {
		t.Fatal("expected seed action to fail")
	}

	st, _, err := loadState(engine.OSFS{}, f.state)
	if err != nil {
		t.Fatal(err)
	}
	if !st["clients/alpha"].PendingSeed {
		t.Fatalf("PendingSeed = false after seed failure, want true (state=%+v)", st)
	}

	// Second run: NewDatabases is empty, but PendingSeed causes seed to retry (without re-running prepare/migrate).
	f.s.NewDatabases = nil
	retryFake := &runnertest.Fake{}
	retryFake.On("sh -c bun run db:seed", runner.Result{}, nil)
	f.git.Fake = retryFake
	f.s.Env = retryFake

	second := f.plan(t)
	want := []string{"clients/alpha: bun run db:seed"}
	if got := summaries(second); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("second plan summaries = %v, want %v", got, want)
	}
	apply(t, second)

	st, _, err = loadState(engine.OSFS{}, f.state)
	if err != nil {
		t.Fatal(err)
	}
	if st["clients/alpha"].PendingSeed {
		t.Fatalf("PendingSeed = true after seed succeeded, want false (state=%+v)", st)
	}

	// Third run: nothing planned.
	if third := f.plan(t); len(third.Actions) != 0 {
		t.Fatalf("third plan summaries = %v, want none", summaries(third))
	}
}

func TestSeedRetriedWhenMigrateFails(t *testing.T) {
	f := newFixture(t, "native", orbitFull)
	f.head("head1")
	f.ok("bun install")
	f.fake.On("sh -c bun run db:migrate", runner.Result{Stderr: "migrate boom\n", ExitCode: 1}, clierr.Tool("migrate exited 1"))
	f.s.NewDatabases = map[string]bool{"app_api": true}

	first := f.plan(t)
	var migrateErr error
	for _, a := range first.Actions {
		if err := a.Do(context.Background()); err != nil {
			migrateErr = err
			break
		}
	}
	if migrateErr == nil {
		t.Fatal("expected migrate action to fail")
	}

	st, _, err := loadState(engine.OSFS{}, f.state)
	if err != nil {
		t.Fatal(err)
	}
	if !st["clients/alpha"].PendingSeed {
		t.Fatalf("PendingSeed = false after migrate failure, want true (state=%+v)", st)
	}

	// Second run: NewDatabases is empty, but PendingSeed triggers migrate and seed.
	f.s.NewDatabases = nil
	retryFake := &runnertest.Fake{}
	retryFake.On("git -C", runner.Result{Stdout: "head1\n"}, nil)
	retryFake.On("sh -c bun run db:migrate", runner.Result{}, nil)
	retryFake.On("sh -c bun run db:seed", runner.Result{}, nil)
	f.git.Fake = retryFake
	f.s.Env = retryFake

	second := f.plan(t)
	want := []string{"clients/alpha: bun run db:migrate", "clients/alpha: bun run db:seed"}
	if got := summaries(second); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("second plan summaries = %v, want %v", got, want)
	}
	apply(t, second)

	st, _, err = loadState(engine.OSFS{}, f.state)
	if err != nil {
		t.Fatal(err)
	}
	if st["clients/alpha"].PendingSeed {
		t.Fatalf("PendingSeed = true after retry succeeded, want false (state=%+v)", st)
	}
}

func TestRunCommandSetsGoToolchainAuto(t *testing.T) {
	t.Run("prepends GOTOOLCHAIN=auto when unset", func(t *testing.T) {
		t.Setenv("GOTOOLCHAIN", "")
		f := newFixture(t, "native", "version: 1\nprepare:\n  - go test ./...\n")
		write(t, filepath.Join(f.repo, ".env"), "FOO=bar\n")
		f.ok("go test ./...")

		apply(t, f.plan(t))
		calls := shCalls(f.fake)
		if len(calls) != 1 {
			t.Fatalf("sh calls = %d, want 1", len(calls))
		}
		got := calls[0].Env
		if len(got) == 0 || got[0] != "GOTOOLCHAIN=auto" {
			t.Fatalf("env = %v, want GOTOOLCHAIN=auto prepended", got)
		}
	})

	t.Run("does not override host GOTOOLCHAIN", func(t *testing.T) {
		t.Setenv("GOTOOLCHAIN", "local")
		f := newFixture(t, "native", "version: 1\nprepare:\n  - go test ./...\n")
		f.ok("go test ./...")

		apply(t, f.plan(t))
		calls := shCalls(f.fake)
		if len(calls) != 1 {
			t.Fatalf("sh calls = %d, want 1", len(calls))
		}
		if hasEnvKey(calls[0].Env, "GOTOOLCHAIN") {
			t.Fatalf("env = %v, did not expect GOTOOLCHAIN when host sets it", calls[0].Env)
		}
	})

	t.Run("does not override env file GOTOOLCHAIN", func(t *testing.T) {
		t.Setenv("GOTOOLCHAIN", "")
		f := newFixture(t, "native", "version: 1\nprepare:\n  - go test ./...\nenv:\n  - schema: .env.schema.yaml\n    write: .env\n")
		write(t, filepath.Join(f.repo, ".env"), "GOTOOLCHAIN=go1.25.0\n")
		f.ok("go test ./...")

		apply(t, f.plan(t))
		calls := shCalls(f.fake)
		if len(calls) != 1 {
			t.Fatalf("sh calls = %d, want 1", len(calls))
		}
		if len(calls[0].Env) != 1 || calls[0].Env[0] != "GOTOOLCHAIN=go1.25.0" {
			t.Fatalf("env = %v, want [GOTOOLCHAIN=go1.25.0]", calls[0].Env)
		}
	})
}
