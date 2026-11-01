package workspace

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/workspace/application"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/gitcli"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/manifestfile"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/osfs"
)

type recordingRunner struct {
	inner runner.Runner

	mu    sync.Mutex
	calls []string
}

func (r *recordingRunner) Run(ctx context.Context, dir, name string, args ...string) (runner.Result, error) {
	r.mu.Lock()
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	r.mu.Unlock()
	return r.inner.Run(ctx, dir, name, args...)
}

func (r *recordingRunner) callsContaining(fragment string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, call := range r.calls {
		if strings.Contains(call, fragment) {
			out = append(out, call)
		}
	}
	return out
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=", "-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func createRepo(t *testing.T, dir, branch string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", branch)
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "commit.gpgSign", "false")
	runGit(t, dir, "config", "core.hooksPath", "")
}

func createBareRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "--bare", "-b", "main")
	runGit(t, dir, "config", "core.hooksPath", "")
}

func seedRemote(t *testing.T, remote string) string {
	t.Helper()
	seed := filepath.Join(t.TempDir(), "seed")
	createRepo(t, seed, "main")
	if err := os.WriteFile(filepath.Join(seed, "file.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "file.txt")
	runGit(t, seed, "commit", "-m", "v1")
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "-u", "origin", "main")
	return seed
}

func writeManifest(t *testing.T, root, remote string, entries ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: \"1\"\nworkspace: test\ngroups:\n  orbit:\n    path: orbit\n    repositories:\n")
	for i := 0; i+1 < len(entries); i += 2 {
		b.WriteString("      - name: " + entries[i] + "\n")
		b.WriteString("        path: " + entries[i+1] + "\n")
		b.WriteString("        remote_url: " + remote + "\n")
		b.WriteString("        default_branch: main\n")
	}
	path := filepath.Join(root, "workspace.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newRealService(rec *recordingRunner) *application.Service {
	return &application.Service{
		Git:      gitcli.New(rec),
		FS:       osfs.New(),
		Manifest: manifestfile.New(),
		Out:      io.Discard,
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	createBareRepo(t, remote)
	seed := seedRemote(t, remote)
	manifestPath := writeManifest(t, root, remote, "repo", "repo")

	rec := &recordingRunner{inner: runner.Exec{}}
	svc := newRealService(rec)

	initOut, err := svc.Init(context.Background(), application.InitInput{
		WorkspaceRoot: root,
		ManifestPath:  manifestPath,
		Scope:         "all",
		Concurrency:   1,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if initOut.Cloned != 1 {
		t.Fatalf("Init output = %+v", initOut)
	}

	statusOut, err := svc.Status(context.Background(), application.StatusInput{
		WorkspaceRoot: root,
		ManifestPath:  manifestPath,
		Scope:         "all",
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statusOut.Repos) != 1 || !statusOut.Repos[0].IsClean || statusOut.Repos[0].CurrentBranch != "main" {
		t.Fatalf("status = %+v", statusOut.Repos)
	}

	if err := os.WriteFile(filepath.Join(seed, "file.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "file.txt")
	runGit(t, seed, "commit", "-m", "v2")
	runGit(t, seed, "push", "origin", "main")

	syncOut, err := svc.Sync(context.Background(), application.SyncInput{
		WorkspaceRoot: root,
		ManifestPath:  manifestPath,
		Scope:         "all",
		Concurrency:   1,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(syncOut.Results) != 1 || !syncOut.Results[0].FastForwarded {
		t.Fatalf("sync = %+v", syncOut.Results)
	}
	data, err := os.ReadFile(filepath.Join(root, "orbit", "repo", "file.txt"))
	if err != nil || string(data) != "v2\n" {
		t.Fatalf("file = %q, err = %v", data, err)
	}
}

func TestStatusDistinguishesMissingAndGitless(t *testing.T) {
	root := t.TempDir()
	gitless := filepath.Join(root, "orbit", "gitless")
	if err := os.MkdirAll(gitless, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitless, "code.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := writeManifest(t, root, "https://example.com/missing.git", "gone", "gone", "gitless", "gitless")

	svc := newRealService(&recordingRunner{inner: runner.Exec{}})
	out, err := svc.Status(context.Background(), application.StatusInput{
		WorkspaceRoot: root,
		ManifestPath:  manifestPath,
		Scope:         "all",
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if out.Repos[0].Error != "missing" {
		t.Errorf("missing = %+v", out.Repos[0])
	}
	if out.Repos[1].Error != "not a git repository" {
		t.Errorf("gitless = %+v", out.Repos[1])
	}
}

func TestRepairIsNonDestructive(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	createBareRepo(t, remote)
	seedRemote(t, remote)

	dest := filepath.Join(root, "orbit", "gitless")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dest, "local.txt")
	if err := os.WriteFile(local, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := writeManifest(t, root, remote, "gitless", "gitless")

	rec := &recordingRunner{inner: runner.Exec{}}
	svc := newRealService(rec)
	out, err := svc.Repair(context.Background(), application.RepairInput{
		WorkspaceRoot: root,
		ManifestPath:  manifestPath,
		Scope:         "all",
		Concurrency:   1,
	})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if out.Repaired != 1 {
		t.Fatalf("repair output = %+v", out)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatalf("expected .git after repair: %v", err)
	}
	data, err := os.ReadFile(local)
	if err != nil || string(data) != "keep me\n" {
		t.Fatalf("local file = %q, err = %v", data, err)
	}
	for _, fragment := range []string{"checkout", "reset"} {
		if calls := rec.callsContaining(fragment); len(calls) > 0 {
			t.Errorf("repair ran destructive command %q: %v", fragment, calls)
		}
	}
}
