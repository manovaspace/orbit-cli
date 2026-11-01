package application

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

type fakeGit struct {
	workTree map[string]bool
	branch   map[string]string
	status   map[string]string
	ahead    map[string][2]int

	workTreeErr error
	branchErr   error
	statusErr   error
	aheadErr    error
	fetchErr    error
	mergeOutput string
	mergeErr    error
	cloneOutput string
	cloneErr    error

	mu      sync.Mutex
	fetched []string
	merged  []string
	cloned  []string
}

func (g *fakeGit) IsWorkTree(_ context.Context, dir string) (bool, error) {
	if g.workTreeErr != nil {
		return false, g.workTreeErr
	}
	return g.workTree[dir], nil
}

func (g *fakeGit) Branch(_ context.Context, dir string) (string, error) {
	if g.branchErr != nil {
		return "", g.branchErr
	}
	return g.branch[dir], nil
}

func (g *fakeGit) Status(_ context.Context, dir string) (string, error) {
	if g.statusErr != nil {
		return "", g.statusErr
	}
	return g.status[dir], nil
}

func (g *fakeGit) AheadBehind(_ context.Context, dir string) (int, int, error) {
	if g.aheadErr != nil {
		return 0, 0, g.aheadErr
	}
	counts := g.ahead[dir]
	return counts[0], counts[1], nil
}

func (g *fakeGit) Fetch(_ context.Context, dir string) error {
	g.mu.Lock()
	g.fetched = append(g.fetched, dir)
	g.mu.Unlock()
	return g.fetchErr
}

func (g *fakeGit) MergeFFOnly(_ context.Context, dir, ref string) (string, error) {
	g.mu.Lock()
	g.merged = append(g.merged, dir+" "+ref)
	g.mu.Unlock()
	return g.mergeOutput, g.mergeErr
}

func (g *fakeGit) Clone(_ context.Context, dir, remoteURL, dest string) (string, error) {
	g.mu.Lock()
	g.cloned = append(g.cloned, remoteURL+" -> "+dest)
	g.mu.Unlock()
	return g.cloneOutput, g.cloneErr
}

type fakeFS struct {
	entries  map[string]bool // path -> isDir
	children map[string][]string

	tempDir string
	tempErr error
	copyErr error

	removed []string
	copies  map[string]string
}

func newFakeFS() *fakeFS {
	return &fakeFS{entries: map[string]bool{}, children: map[string][]string{}, copies: map[string]string{}}
}

func (f *fakeFS) Stat(path string) (FileInfo, bool, error) {
	isDir, ok := f.entries[path]
	if !ok {
		return FileInfo{}, false, nil
	}
	return FileInfo{IsDir: isDir}, true, nil
}

func (f *fakeFS) ReadDir(path string) ([]string, error) {
	return f.children[path], nil
}

func (f *fakeFS) MkdirAll(string) error { return nil }

func (f *fakeFS) TempDir(string) (string, error) {
	return f.tempDir, f.tempErr
}

func (f *fakeFS) RemoveAll(path string) error {
	f.removed = append(f.removed, path)
	return nil
}

func (f *fakeFS) CopyTree(src, dst string) error {
	if f.copyErr != nil {
		return f.copyErr
	}
	f.copies[dst] = src
	return nil
}

type fakeManifest struct {
	m    *manifest.WorkspaceManifest
	err  error
	load string
}

func (f *fakeManifest) Load(path string) (*manifest.WorkspaceManifest, error) {
	f.load = path
	return f.m, f.err
}

func parseManifest(t *testing.T, yaml string) *manifest.WorkspaceManifest {
	t.Helper()
	m, err := manifest.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

func newTestService(git Git, fs FS, m Manifest) *Service {
	return &Service{Git: git, FS: fs, Manifest: m}
}

func TestStatusClassifiesRepositories(t *testing.T) {
	const yaml = `version: "1"
workspace: test
groups:
  test:
    repositories:
      - name: clean
        path: clean
      - name: dirty
        path: dirty
      - name: missing
        path: missing
      - name: gitless
        path: gitless
`
	m := parseManifest(t, yaml)
	fs := newFakeFS()
	fs.entries["/ws/workspace.yaml"] = false
	fs.entries["/ws/clean"] = true
	fs.entries["/ws/dirty"] = true
	fs.entries["/ws/gitless"] = true

	git := &fakeGit{
		workTree: map[string]bool{"/ws/clean": true, "/ws/dirty": true},
		branch:   map[string]string{"/ws/clean": "main", "/ws/dirty": "feature"},
		status:   map[string]string{"/ws/dirty": " M file.txt\n?? new.txt\n"},
		ahead:    map[string][2]int{"/ws/clean": {1, 2}},
	}

	res, err := newTestService(git, fs, &fakeManifest{m: m}).Status(context.Background(), StatusInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
		Scope:         "all",
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(res.Repos) != 4 {
		t.Fatalf("got %d repos, want 4", len(res.Repos))
	}
	if res.Repos[0].Error != "" || !res.Repos[0].IsClean || res.Repos[0].CurrentBranch != "main" {
		t.Errorf("clean repo = %+v", res.Repos[0])
	}
	if res.Repos[0].AheadCount != 1 || res.Repos[0].BehindCount != 2 {
		t.Errorf("ahead/behind = %d/%d", res.Repos[0].AheadCount, res.Repos[0].BehindCount)
	}
	if res.Repos[1].Error != "" || res.Repos[1].IsClean || res.Repos[1].ModifiedCount != 2 {
		t.Errorf("dirty repo = %+v", res.Repos[1])
	}
	if res.Repos[2].Error != domain.ErrMissing {
		t.Errorf("missing repo error = %q", res.Repos[2].Error)
	}
	if res.Repos[3].Error != domain.ErrGitless {
		t.Errorf("gitless repo error = %q", res.Repos[3].Error)
	}
}

func TestStatusMissingManifest(t *testing.T) {
	fs := newFakeFS()
	_, err := newTestService(&fakeGit{}, fs, &fakeManifest{}).Status(context.Background(), StatusInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
		Scope:         "all",
	})
	if err == nil || !strings.Contains(err.Error(), "manifest file not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestSyncReasons(t *testing.T) {
	const yaml = `version: "1"
workspace: test
groups:
  test:
    repositories:
      - name: ff
        path: ff
      - name: current
        path: current
      - name: dirty
        path: dirty
      - name: branch
        path: branch
      - name: diverged
        path: diverged
      - name: missing
        path: missing
`
	m := parseManifest(t, yaml)
	fs := newFakeFS()
	fs.entries["/ws/workspace.yaml"] = false
	for _, name := range []string{"ff", "current", "dirty", "branch", "diverged"} {
		fs.entries[filepath.Join("/ws", name)] = true
		fs.entries[filepath.Join("/ws", name, ".git")] = true
	}
	git := &fakeGit{
		workTree: map[string]bool{
			"/ws/ff": true, "/ws/current": true, "/ws/dirty": true, "/ws/branch": true, "/ws/diverged": true,
		},
		branch: map[string]string{
			"/ws/ff": "main", "/ws/current": "main", "/ws/dirty": "main", "/ws/branch": "feature", "/ws/diverged": "main",
		},
		status: map[string]string{"/ws/dirty": " M file.txt\n"},
	}
	// First merge fast-forwards, the current one is already up to date, and the
	// diverged one fails.
	git.mergeOutput = "Already up to date."
	git.mergeErr = nil

	res, err := newTestService(git, fs, &fakeManifest{m: m}).Sync(context.Background(), SyncInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
		Scope:         "all",
		Concurrency:   2,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.Results) != 6 {
		t.Fatalf("got %d results, want 6", len(res.Results))
	}
	if !res.Results[0].Success || res.Results[0].FastForwarded {
		t.Errorf("already up to date = %+v", res.Results[0])
	}
	if res.Results[2].SkippedReason != domain.SkipUncommitted {
		t.Errorf("dirty skip = %+v", res.Results[2])
	}
	if res.Results[3].SkippedReason != domain.NotDefaultBranch("feature", "main") {
		t.Errorf("branch skip = %+v", res.Results[3])
	}
	if res.Results[5].Error != domain.NotCloned {
		t.Errorf("missing = %+v", res.Results[5])
	}
}

func TestSyncReportsFetchAndMergeFailures(t *testing.T) {
	const yaml = `version: "1"
workspace: test
groups:
  test:
    repositories:
      - name: fetch
        path: fetch
      - name: merge
        path: merge
`
	m := parseManifest(t, yaml)
	fs := newFakeFS()
	fs.entries["/ws/workspace.yaml"] = false
	for _, name := range []string{"fetch", "merge"} {
		fs.entries[filepath.Join("/ws", name)] = true
		fs.entries[filepath.Join("/ws", name, ".git")] = true
	}

	base := &fakeGit{
		workTree: map[string]bool{"/ws/fetch": true, "/ws/merge": true},
		branch:   map[string]string{"/ws/fetch": "main", "/ws/merge": "main"},
	}
	service := newTestService(base, fs, &fakeManifest{m: m})

	base.fetchErr = errors.New("boom")
	res, err := service.Sync(context.Background(), SyncInput{WorkspaceRoot: "/ws", ManifestPath: "/ws/workspace.yaml", Scope: "all"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !strings.Contains(res.Results[0].Error, "git fetch origin failed: boom") {
		t.Errorf("fetch error = %q", res.Results[0].Error)
	}

	base.fetchErr = nil
	base.mergeErr = errors.New("exit 1")
	base.mergeOutput = "CONFLICT"
	res, err = service.Sync(context.Background(), SyncInput{WorkspaceRoot: "/ws", ManifestPath: "/ws/workspace.yaml", Scope: "all"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Results[1].SkippedReason != domain.SkipNotFastForward || res.Results[1].Error != "CONFLICT" {
		t.Errorf("merge result = %+v", res.Results[1])
	}
}

func TestRepairCopiesGitWithoutRemovingWorkingTree(t *testing.T) {
	const yaml = `version: "1"
workspace: test
groups:
  test:
    repositories:
      - name: orbit-frontend
        path: orbit-frontend
`
	m := parseManifest(t, yaml)
	fs := newFakeFS()
	fs.entries["/ws/workspace.yaml"] = false
	fs.entries["/ws/orbit-frontend"] = true
	fs.entries["/tmp/orbit-repair-1"] = true
	fs.entries["/tmp/orbit-repair-1/clone/.git"] = true
	fs.tempDir = "/tmp/orbit-repair-1"

	git := &fakeGit{}
	res, err := newTestService(git, fs, &fakeManifest{m: m}).Repair(context.Background(), RepairInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
		Scope:         "all",
	})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if len(res.Results) != 1 || !res.Results[0].Success || res.Results[0].Skipped != "" {
		t.Fatalf("result = %+v", res.Results)
	}
	if got := fs.copies["/ws/orbit-frontend/.git"]; got != "/tmp/orbit-repair-1/clone/.git" {
		t.Errorf("copied .git from %q", got)
	}
	if len(fs.removed) != 1 || fs.removed[0] != "/tmp/orbit-repair-1" {
		t.Errorf("removed = %v (only the temporary clone may be removed)", fs.removed)
	}
	if len(git.merged) != 0 {
		t.Errorf("repair must not merge: %v", git.merged)
	}
}

func TestRepairSkipsExistingGit(t *testing.T) {
	const yaml = `version: "1"
workspace: test
groups:
  test:
    repositories:
      - name: repo
        path: repo
`
	m := parseManifest(t, yaml)
	fs := newFakeFS()
	fs.entries["/ws/workspace.yaml"] = false
	fs.entries["/ws/repo"] = true
	fs.entries["/ws/repo/.git"] = true

	res, err := newTestService(&fakeGit{}, fs, &fakeManifest{m: m}).Repair(context.Background(), RepairInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
		Scope:         "all",
	})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.Results[0].Skipped != "already a git repository" {
		t.Errorf("result = %+v", res.Results[0])
	}
}

func TestInitCloneOutcomes(t *testing.T) {
	const yaml = `version: "1"
workspace: test
groups:
  test:
    repositories:
      - name: existing
        path: existing
      - name: fresh
        path: fresh
      - name: gitless
        path: gitless
`
	m := parseManifest(t, yaml)
	fs := newFakeFS()
	fs.entries["/ws/workspace.yaml"] = false
	fs.entries["/ws/existing"] = true
	fs.entries["/ws/existing/.git"] = true
	fs.entries["/ws/gitless"] = true
	fs.children["/ws/gitless"] = []string{"code.go"}

	res, err := newTestService(&fakeGit{}, fs, &fakeManifest{m: m}).Init(context.Background(), InitInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
		Scope:         "all",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if res.AlreadyPresent != 1 || res.Cloned != 1 || res.Failed != 1 {
		t.Errorf("counts = %+v", res)
	}
	if res.Results[2].Error != "exists but is not a git repository; run orbit repair" {
		t.Errorf("gitless error = %q", res.Results[2].Error)
	}
}

func TestInitWrapsResolveError(t *testing.T) {
	fs := newFakeFS()
	fs.entries["/ws/workspace.yaml"] = false
	m := parseManifest(t, "version: \"1\"\nworkspace: test\ngroups: {}\n")

	_, err := newTestService(&fakeGit{}, fs, &fakeManifest{m: m}).Init(context.Background(), InitInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
		Scope:         "unknown",
	})
	if err == nil || !strings.Contains(err.Error(), `failed to resolve repositories for scope "unknown"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestSyncAllMissingManifest(t *testing.T) {
	fs := newFakeFS()
	res, err := newTestService(&fakeGit{}, fs, &fakeManifest{}).SyncAll(context.Background(), SyncAllInput{
		WorkspaceRoot: "/ws",
		ManifestPath:  "/ws/workspace.yaml",
	})
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if res.ManifestFound {
		t.Error("expected ManifestFound=false")
	}
}
