package handbook

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

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const root = "/ws"

// fakeFS is a map-backed engine.FS. files maps each existing path to whether it is a directory.
// Only the methods the handbook part and the engine's manifest reload use are implemented.
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

func newFS(paths map[string]bool) *fakeFS {
	files := map[string]bool{root: true}
	for p, isDir := range paths {
		files[p] = isDir
	}
	return &fakeFS{files: files}
}

func newRun() *runnertest.Fake {
	run := &runnertest.Fake{}
	run.On("git clone", runner.Result{}, nil)
	return run
}

func TestHandbookClonesWithProtocol(t *testing.T) {
	cases := []struct {
		protocol string
		url      string
	}{
		{"https", "https://github.com/manovaspace/handbook.git"},
		{"ssh", "git@github.com:manovaspace/handbook.git"},
	}
	for _, c := range cases {
		t.Run(c.protocol, func(t *testing.T) {
			fsys := newFS(nil)
			run := newRun()
			s := &engine.State{Root: root, Machine: spec.Machine{GitProtocol: c.protocol}, Run: run, Env: run, FS: fsys}

			plan, err := part{}.Plan(context.Background(), s)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if len(plan.Actions) != 1 || plan.Actions[0].Summary != "Clone handbook" {
				t.Fatalf("actions = %+v, want one Clone handbook", plan.Actions)
			}
			if err := plan.Actions[0].Do(context.Background()); err != nil {
				t.Fatalf("Do: %v", err)
			}
			calls := run.Calls()
			if len(calls) != 1 {
				t.Fatalf("calls = %+v, want one git clone", calls)
			}
			want := []string{"clone", "--filter=blob:none", c.url, filepath.Join(root, "handbook")}
			if calls[0].Name != "git" || !reflect.DeepEqual(calls[0].Args, want) {
				t.Fatalf("clone = %s %v, want git %v", calls[0].Name, calls[0].Args, want)
			}
			if !reflect.DeepEqual(calls[0].Env, []string{"GIT_TERMINAL_PROMPT=0"}) {
				t.Fatalf("clone env = %v, want GIT_TERMINAL_PROMPT=0", calls[0].Env)
			}
			if !reflect.DeepEqual(fsys.made, []string{root}) {
				t.Fatalf("MkdirAll = %v, want the workspace root", fsys.made)
			}
		})
	}
}

func TestHandbookRemovesPartialDir(t *testing.T) {
	handbook := filepath.Join(root, "handbook")
	cases := map[string]map[string]bool{
		"empty dir":         {handbook: true},
		"interrupted clone": {handbook: true, filepath.Join(handbook, ".git"): true},
	}
	for name, paths := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := newFS(paths)
			run := newRun()
			s := &engine.State{Root: root, Machine: spec.Machine{GitProtocol: "https"}, Run: run, Env: run, FS: fsys}

			plan, err := part{}.Plan(context.Background(), s)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if len(plan.Findings) != 0 || len(plan.Actions) != 1 {
				t.Fatalf("plan = %+v, want one clone action and no findings", plan)
			}
			if err := plan.Actions[0].Do(context.Background()); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if !reflect.DeepEqual(fsys.removed, []string{handbook}) {
				t.Fatalf("RemoveAll = %v, want %s", fsys.removed, handbook)
			}
			if len(run.Calls()) != 1 {
				t.Fatalf("git calls = %+v, want one clone after removal", run.Calls())
			}
		})
	}
}

func TestHandbookRefusesForeignDir(t *testing.T) {
	handbook := filepath.Join(root, "handbook")
	fsys := newFS(map[string]bool{handbook: true, filepath.Join(handbook, "README.md"): false})
	run := newRun()
	s := &engine.State{Root: root, Machine: spec.Machine{GitProtocol: "https"}, Run: run, Env: run, FS: fsys}

	plan, err := part{}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %+v, want none", plan.Actions)
	}
	if len(plan.Findings) != 1 {
		t.Fatalf("findings = %+v, want one", plan.Findings)
	}
	f := plan.Findings[0]
	want := "handbook/ exists but is not a git checkout: move it aside and rerun o setup"
	if f.Severity != engine.SeverityError || f.Message != want {
		t.Fatalf("finding = %+v, want error %q", f, want)
	}
	if len(fsys.removed) != 0 {
		t.Fatalf("RemoveAll called on a foreign directory: %v", fsys.removed)
	}
}

func TestHandbookAlreadyCloned(t *testing.T) {
	handbook := filepath.Join(root, "handbook")
	fsys := newFS(map[string]bool{handbook: true, filepath.Join(handbook, ".git"): true, filepath.Join(handbook, ".git", "HEAD"): false})
	s := &engine.State{Root: root, Machine: spec.Machine{GitProtocol: "https"}, Run: newRun(), FS: fsys}

	plan, err := part{}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 || len(plan.Findings) != 0 {
		t.Fatalf("plan = %+v, want empty", plan)
	}
}

func TestHandbookRegularFileIsFinding(t *testing.T) {
	fsys := newFS(map[string]bool{filepath.Join(root, "handbook"): false})
	s := &engine.State{Root: root, Machine: spec.Machine{GitProtocol: "https"}, Run: newRun(), Env: newRun(), FS: fsys}

	plan, err := part{}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v (want a finding, not an error)", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %+v, want none", plan.Actions)
	}
	if len(plan.Findings) != 1 || plan.Findings[0].Severity != engine.SeverityError ||
		plan.Findings[0].Message != "handbook/ exists but is not a git checkout: move it aside and rerun o setup" {
		t.Fatalf("findings = %+v, want the not-a-checkout error", plan.Findings)
	}
	if len(fsys.removed) != 0 {
		t.Fatalf("RemoveAll called on a regular file: %v", fsys.removed)
	}
}
