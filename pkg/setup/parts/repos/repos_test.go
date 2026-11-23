package repos

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const root = "/ws"

// fakeFS is a map-backed engine.FS. files maps each existing path to whether it is a directory.
type fakeFS struct {
	engine.FS
	files   map[string]bool
	removed []string
	made    []string
}

type fakeInfo struct {
	name string
	dir  bool
}

func (i fakeInfo) Name() string { return i.name }
func (i fakeInfo) Size() int64  { return 0 }
func (i fakeInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.dir }
func (i fakeInfo) Sys() any           { return nil }

func (f *fakeFS) Stat(path string) (fs.FileInfo, error) {
	for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if isDir, ok := f.files[dir]; ok && !isDir {
			return nil, syscall.ENOTDIR
		}
	}
	isDir, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return fakeInfo{name: filepath.Base(path), dir: isDir}, nil
}

func (f *fakeFS) ReadDir(dir string) ([]os.DirEntry, error) {
	isDir, ok := f.files[dir]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if !isDir {
		return nil, errors.New("not a directory")
	}
	var out []os.DirEntry
	for path, d := range f.files {
		if filepath.Dir(path) == dir {
			out = append(out, fs.FileInfoToDirEntry(fakeInfo{name: filepath.Base(path), dir: d}))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (f *fakeFS) MkdirAll(path string, _ fs.FileMode) error {
	f.files[path] = true
	f.made = append(f.made, path)
	return nil
}

func (f *fakeFS) RemoveAll(path string) error {
	f.removed = append(f.removed, path)
	for p := range f.files {
		if p == path || strings.HasPrefix(p, path+"/") {
			delete(f.files, p)
		}
	}
	return nil
}

// tree merges path sets into one, with the workspace root always present.
func tree(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{root: true}
	for _, set := range sets {
		for p, isDir := range set {
			out[p] = isDir
		}
	}
	return out
}

// checkout is the path set of a git checkout at dir.
func checkout(dir string) map[string]bool {
	return map[string]bool{
		dir:                                true,
		filepath.Join(dir, ".git"):         true,
		filepath.Join(dir, ".git", "HEAD"): false,
	}
}

func newFS(paths map[string]bool) *fakeFS {
	return &fakeFS{files: tree(paths)}
}

// testManifest has three repositories under clients/. Project app selects alpha and beta,
// project solo selects alpha only.
func testManifest() *manifest.WorkspaceManifest {
	repo := func(name string) manifest.RepoConfig {
		return manifest.RepoConfig{
			Name:          name,
			Path:          "clients/" + name,
			RemoteURL:     "git@github.com:manovaspace/" + name + ".git",
			DefaultBranch: "main",
		}
	}
	return &manifest.WorkspaceManifest{
		Groups: map[string]manifest.GroupConfig{
			"clients": {Repositories: []manifest.RepoConfig{repo("alpha"), repo("beta"), repo("gamma")}},
		},
		Projects: map[string]manifest.ProjectConfig{
			"app":  {Repos: []string{"clients/alpha", "clients/beta"}},
			"solo": {Repos: []string{"clients/alpha"}},
		},
	}
}

func newState(fsys *fakeFS, run *runnertest.Fake, project string) *engine.State {
	return &engine.State{
		Root:     root,
		Machine:  spec.Machine{GitProtocol: "https", Projects: []string{project}},
		Manifest: testManifest(),
		Run:      run,
		Env:      run,
		FS:       fsys,
	}
}

func lsRemoteURL(name string) string {
	return "https://github.com/manovaspace/" + name + ".git"
}

func answerLsRemote(run *runnertest.Fake, name string, res runner.Result, err error) {
	run.On("git ls-remote --heads "+lsRemoteURL(name), res, err)
}

// answerUpToDate scripts a present checkout at dir with hooks set, on main, clean and level with
// its upstream.
func answerUpToDate(run *runnertest.Fake, dir string) {
	run.On("git -C "+dir+" config --local core.hooksPath", runner.Result{Stdout: "/ws/.githooks\n"}, nil)
	run.On("git -C "+dir+" symbolic-ref --short HEAD", runner.Result{Stdout: "main\n"}, nil)
	run.On("git -C "+dir+" status --porcelain", runner.Result{}, nil)
	run.On("git -C "+dir+" fetch --quiet origin main", runner.Result{}, nil)
	run.On("git -C "+dir+" rev-list --count HEAD..@{u}", runner.Result{Stdout: "0\n"}, nil)
}

func planOf(t *testing.T, s *engine.State) engine.Plan {
	t.Helper()
	plan, err := part{}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return plan
}

func doAll(t *testing.T, plan engine.Plan) {
	t.Helper()
	for _, a := range plan.Actions {
		if err := a.Do(context.Background()); err != nil {
			t.Fatalf("Do %q: %v", a.Summary, err)
		}
	}
}

// calls returns each recorded command as one line: name and arguments.
func calls(run *runnertest.Fake) []string {
	var out []string
	for _, c := range run.Calls() {
		out = append(out, strings.Join(append([]string{c.Name}, c.Args...), " "))
	}
	return out
}

func summaries(plan engine.Plan) []string {
	var out []string
	for _, a := range plan.Actions {
		out = append(out, a.Summary)
	}
	return out
}

func TestReposClonesMissingBlobless(t *testing.T) {
	fsys := newFS(nil)
	run := &runnertest.Fake{}
	answerLsRemote(run, "alpha", runner.Result{}, nil)
	run.On("git clone", runner.Result{}, nil)
	run.On("git -C /ws/clients/alpha config", runner.Result{}, nil)
	s := newState(fsys, run, "solo")

	plan := planOf(t, s)
	if !reflect.DeepEqual(summaries(plan), []string{"Clone clients/alpha"}) || len(plan.Findings) != 0 {
		t.Fatalf("plan = %+v, want one clone of clients/alpha and no findings", plan)
	}
	doAll(t, plan)

	want := []string{
		"git ls-remote --heads https://github.com/manovaspace/alpha.git",
		"git clone --filter=blob:none https://github.com/manovaspace/alpha.git /ws/clients/alpha",
		"git -C /ws/clients/alpha config --local core.hooksPath /ws/.githooks",
	}
	if got := calls(run); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for _, c := range run.Calls() {
		if len(c.Args) > 0 && (c.Args[0] == "ls-remote" || c.Args[0] == "clone") {
			if !reflect.DeepEqual(c.Env, []string{"GIT_TERMINAL_PROMPT=0"}) {
				t.Fatalf("%s env = %v, want GIT_TERMINAL_PROMPT=0", c.Args[0], c.Env)
			}
		}
	}
	if !reflect.DeepEqual(fsys.made, []string{"/ws/clients"}) {
		t.Fatalf("MkdirAll = %v, want the parent /ws/clients", fsys.made)
	}
}

func TestReposNoAccessIsWarning(t *testing.T) {
	fsys := newFS(nil)
	run := &runnertest.Fake{}
	answerLsRemote(run, "alpha", runner.Result{ExitCode: 128, Stderr: "remote: Repository not found.\nfatal: repository not found"}, errors.New("exit status 128"))
	answerLsRemote(run, "beta", runner.Result{}, nil)
	run.On("git clone", runner.Result{}, nil)
	run.On("git -C /ws/clients/beta config", runner.Result{}, nil)
	s := newState(fsys, run, "app")

	plan := planOf(t, s)
	if !reflect.DeepEqual(summaries(plan), []string{"Clone clients/beta"}) {
		t.Fatalf("actions = %v, want only the clone of clients/beta", summaries(plan))
	}
	want := []engine.Finding{{
		Part:     "repos",
		Subject:  "clients/alpha",
		Message:  "no access to alpha (ask an admin)",
		Severity: engine.SeverityWarn,
	}}
	if !reflect.DeepEqual(plan.Findings, want) {
		t.Fatalf("findings = %+v, want one warning for alpha", plan.Findings)
	}
}

func TestReposAllInaccessibleFails(t *testing.T) {
	denied := runner.Result{ExitCode: 128, Stderr: "fatal: could not read Username for 'https://github.com': terminal prompts disabled"}
	cases := map[string]func(run *runnertest.Fake, fsys *fakeFS){
		"every clone denied": func(run *runnertest.Fake, fsys *fakeFS) {
			answerLsRemote(run, "alpha", runner.Result{ExitCode: 128, Stderr: "remote: Repository not found."}, errors.New("exit status 128"))
			answerLsRemote(run, "beta", denied, errors.New("exit status 128"))
		},
		"present repo counts as accessible": func(run *runnertest.Fake, fsys *fakeFS) {
			fsys.files = tree(fsys.files, checkout("/ws/clients/beta"))
			answerUpToDate(run, "/ws/clients/beta")
			answerLsRemote(run, "alpha", runner.Result{ExitCode: 128, Stderr: "remote: Repository not found."}, errors.New("exit status 128"))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := newFS(nil)
			run := &runnertest.Fake{}
			setup(run, fsys)
			s := newState(fsys, run, "app")

			plan := planOf(t, s)
			if len(plan.Actions) != 0 {
				t.Fatalf("actions = %v, want none", summaries(plan))
			}
			var errs []engine.Finding
			for _, f := range plan.Findings {
				if f.Severity == engine.SeverityError {
					errs = append(errs, f)
				}
			}
			if name == "every clone denied" {
				if len(errs) != 1 || errs[0].Message != "no access to any selected repository" {
					t.Fatalf("error findings = %+v, want one no-access error", errs)
				}
				if len(plan.Findings) != 3 {
					t.Fatalf("findings = %+v, want two warnings and one error", plan.Findings)
				}
			} else if len(errs) != 0 {
				t.Fatalf("error findings = %+v, want none when a present repo is accessible", errs)
			}
		})
	}
}

func TestReposLsRemoteOtherFailureIsPlanError(t *testing.T) {
	fsys := newFS(nil)
	run := &runnertest.Fake{}
	answerLsRemote(run, "alpha", runner.Result{ExitCode: 128, Stderr: "ssh: Could not resolve hostname github.com"}, errors.New("exit status 128"))
	s := newState(fsys, run, "solo")

	if _, err := (part{}).Plan(context.Background(), s); err == nil {
		t.Fatal("Plan returned nil error for a DNS failure, want the part to fail")
	}
}

func TestReposSetsHooksPath(t *testing.T) {
	setHooks := "git -C /ws/clients/alpha config --local core.hooksPath /ws/.githooks"
	cases := map[string]runner.Result{
		"unset":      {ExitCode: 1},
		"other path": {Stdout: "/old/hooks\n"},
	}
	for name, read := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := newFS(checkout("/ws/clients/alpha"))
			run := &runnertest.Fake{}
			run.On(setHooks, runner.Result{}, nil)
			readErr := error(nil)
			if read.ExitCode != 0 {
				readErr = errors.New("exit status 1")
			}
			run.On("git -C /ws/clients/alpha config --local core.hooksPath", read, readErr)
			answerUpToDate(run, "/ws/clients/alpha")
			s := newState(fsys, run, "solo")

			plan := planOf(t, s)
			if !reflect.DeepEqual(summaries(plan), []string{"Set hooks path for clients/alpha"}) {
				t.Fatalf("actions = %v, want one hooks action", summaries(plan))
			}
			doAll(t, plan)
			if got := calls(run); got[len(got)-1] != setHooks {
				t.Fatalf("last call = %q, want %q", got[len(got)-1], setHooks)
			}
		})
	}
}

func TestReposSetsHandbookHooksPath(t *testing.T) {
	fsys := newFS(tree(checkout("/ws/clients/alpha"), checkout("/ws/handbook")))
	run := &runnertest.Fake{}
	run.On("git -C /ws/handbook config --local core.hooksPath /ws/.githooks", runner.Result{}, nil)
	run.On("git -C /ws/handbook config --local core.hooksPath", runner.Result{ExitCode: 1}, errors.New("exit status 1"))
	answerUpToDate(run, "/ws/clients/alpha")
	s := newState(fsys, run, "solo")

	plan := planOf(t, s)
	if !reflect.DeepEqual(summaries(plan), []string{"Set hooks path for handbook"}) {
		t.Fatalf("actions = %v, want the handbook hooks action", summaries(plan))
	}
}

func TestReposFastForwardsOnlyCleanDefault(t *testing.T) {
	dir := "/ws/clients/alpha"
	cases := []struct {
		name       string
		branch     runner.Result
		branchErr  error
		status     string
		fetch      runner.Result
		fetchErr   error
		behind     string
		wantAction bool
		wantFind   string
		wantFetch  bool
	}{
		{name: "behind default clean", branch: runner.Result{Stdout: "main\n"}, behind: "2\n", wantAction: true, wantFetch: true},
		{name: "feature branch", branch: runner.Result{Stdout: "feature/x\n"}, behind: "2\n"},
		{name: "detached HEAD", branch: runner.Result{ExitCode: 128}, branchErr: errors.New("exit status 128"), behind: "2\n"},
		{name: "dirty tree", branch: runner.Result{Stdout: "main\n"}, status: " M file.go\n", behind: "2\n"},
		{name: "up to date", branch: runner.Result{Stdout: "main\n"}, behind: "0\n", wantFetch: true},
		{
			name: "fetch fails", branch: runner.Result{Stdout: "main\n"},
			fetch:    runner.Result{ExitCode: 128, Stderr: "fatal: unable to access remote\nsecond line"},
			fetchErr: errors.New("exit status 128"), wantFind: "could not fetch clients/alpha: fatal: unable to access remote",
			wantFetch: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := newFS(checkout(dir))
			run := &runnertest.Fake{}
			run.On("git -C "+dir+" config --local core.hooksPath", runner.Result{Stdout: "/ws/.githooks\n"}, nil)
			run.On("git -C "+dir+" symbolic-ref --short HEAD", c.branch, c.branchErr)
			run.On("git -C "+dir+" status --porcelain", runner.Result{Stdout: c.status}, nil)
			run.On("git -C "+dir+" fetch --quiet origin main", c.fetch, c.fetchErr)
			run.On("git -C "+dir+" rev-list --count HEAD..@{u}", runner.Result{Stdout: c.behind}, nil)
			run.On("git -C "+dir+" merge --ff-only @{u}", runner.Result{}, nil)
			s := newState(fsys, run, "solo")

			plan := planOf(t, s)
			if got := len(plan.Actions) == 1; got != c.wantAction {
				t.Fatalf("actions = %v, want fast-forward %v", summaries(plan), c.wantAction)
			}
			if c.wantAction && plan.Actions[0].Summary != "Fast-forward clients/alpha" {
				t.Fatalf("summary = %q", plan.Actions[0].Summary)
			}
			if c.wantFind == "" && len(plan.Findings) != 0 {
				t.Fatalf("findings = %+v, want none", plan.Findings)
			}
			if c.wantFind != "" {
				if len(plan.Findings) != 1 || plan.Findings[0].Message != c.wantFind || plan.Findings[0].Severity != engine.SeverityWarn {
					t.Fatalf("findings = %+v, want warning %q", plan.Findings, c.wantFind)
				}
			}
			hasFetch := false
			for _, line := range calls(run) {
				if strings.Contains(line, " fetch ") {
					hasFetch = true
				}
			}
			if hasFetch != c.wantFetch {
				t.Fatalf("fetch called = %v, want %v", hasFetch, c.wantFetch)
			}
			doAll(t, plan)
			if c.wantAction {
				last := calls(run)[len(calls(run))-1]
				if last != "git -C "+dir+" merge --ff-only @{u}" {
					t.Fatalf("last call = %q, want the ff-only merge", last)
				}
			}
		})
	}
}

func TestReposRecloneAfterInterruptedClone(t *testing.T) {
	dir := "/ws/clients/alpha"
	cases := map[string]map[string]bool{
		"empty dir":         {dir: true},
		"interrupted clone": {dir: true, filepath.Join(dir, ".git"): true},
	}
	for name, paths := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := newFS(paths)
			run := &runnertest.Fake{}
			answerLsRemote(run, "alpha", runner.Result{}, nil)
			run.On("git clone", runner.Result{}, nil)
			run.On("git -C "+dir+" config", runner.Result{}, nil)
			s := newState(fsys, run, "solo")

			plan := planOf(t, s)
			if !reflect.DeepEqual(summaries(plan), []string{"Clone clients/alpha"}) {
				t.Fatalf("actions = %v, want one clone", summaries(plan))
			}
			doAll(t, plan)
			if !reflect.DeepEqual(fsys.removed, []string{dir}) {
				t.Fatalf("RemoveAll = %v, want %s", fsys.removed, dir)
			}
			if got := calls(run); len(got) != 3 || !strings.HasPrefix(got[1], "git clone --filter=blob:none") {
				t.Fatalf("calls = %v, want ls-remote, clone, set hooks", got)
			}
		})
	}

	t.Run("foreign dir is an error", func(t *testing.T) {
		fsys := newFS(map[string]bool{dir: true, filepath.Join(dir, "README.md"): false})
		run := &runnertest.Fake{}
		s := newState(fsys, run, "solo")

		plan := planOf(t, s)
		want := []engine.Finding{{
			Part:     "repos",
			Subject:  "clients/alpha",
			Message:  "clients/alpha/ exists but is not a git checkout: move it aside and rerun o setup",
			Severity: engine.SeverityError,
		}}
		if !reflect.DeepEqual(plan.Findings, want) || len(plan.Actions) != 0 {
			t.Fatalf("plan = %+v, want the not-a-checkout error and no action", plan)
		}
		if len(fsys.removed) != 0 || len(calls(run)) != 0 {
			t.Fatalf("foreign dir touched: removed=%v calls=%v", fsys.removed, calls(run))
		}
	})
}

func TestReposNotSelectedIsWarning(t *testing.T) {
	fsys := newFS(tree(checkout("/ws/clients/alpha"), checkout("/ws/clients/beta")))
	run := &runnertest.Fake{}
	answerUpToDate(run, "/ws/clients/alpha")
	s := newState(fsys, run, "solo")

	plan := planOf(t, s)
	want := []engine.Finding{{
		Part:     "repos",
		Subject:  "clients/beta",
		Message:  "not selected; remove with `o remove` or delete by hand",
		Severity: engine.SeverityWarn,
	}}
	if len(plan.Actions) != 0 || !reflect.DeepEqual(plan.Findings, want) {
		t.Fatalf("plan = %+v, want only the not-selected warning for clients/beta", plan)
	}
}

func TestReposSecondRunNoActions(t *testing.T) {
	fsys := newFS(tree(checkout("/ws/clients/alpha"), checkout("/ws/clients/beta"), checkout("/ws/handbook")))
	run := &runnertest.Fake{}
	answerUpToDate(run, "/ws/clients/alpha")
	answerUpToDate(run, "/ws/clients/beta")
	run.On("git -C /ws/handbook config --local core.hooksPath", runner.Result{Stdout: "/ws/.githooks\n"}, nil)
	s := newState(fsys, run, "app")

	plan := planOf(t, s)
	if len(plan.Actions) != 0 || len(plan.Findings) != 0 {
		t.Fatalf("plan = %+v, want no actions and no findings", plan)
	}
}
