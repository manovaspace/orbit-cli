package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"git.dev.manova.space/manova/orbit-cli/pkg/manifest"
)

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\nOutput: %s", args, dir, err, string(out))
	}
	return string(out)
}

func createTestRepo(t *testing.T, dir string, branch string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	runGitCmd(t, dir, "init", "-b", branch)
	runGitCmd(t, dir, "config", "user.name", "Test User")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "commit.gpgSign", "false")
}

func createBareRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	runGitCmd(t, dir, "init", "--bare")
}

func TestInspectRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Non-existent directory
	t.Run("NonExistentDir", func(t *testing.T) {
		status, err := InspectRepo(filepath.Join(tmpDir, "does-not-exist"))
		if err == nil {
			t.Errorf("expected error for non-existent dir, got nil")
		}
		if status == nil || status.Error == "" {
			t.Errorf("expected status with error message, got %+v", status)
		}
	})

	// 2. Non-git directory
	t.Run("NonGitDir", func(t *testing.T) {
		nonGitDir := filepath.Join(tmpDir, "nongit")
		_ = os.MkdirAll(nonGitDir, 0755)
		status, err := InspectRepo(nonGitDir)
		if err == nil {
			t.Errorf("expected error for non-git dir, got nil")
		}
		if status == nil || status.Error != "not a git repository" {
			t.Errorf("expected 'not a git repository' error, got %+v", status)
		}
	})

	// 3. Clean git repository
	t.Run("CleanRepo", func(t *testing.T) {
		repoDir := filepath.Join(tmpDir, "cleanrepo")
		createTestRepo(t, repoDir, "main")

		readmeFile := filepath.Join(repoDir, "README.md")
		_ = os.WriteFile(readmeFile, []byte("# Clean Repo\n"), 0644)
		runGitCmd(t, repoDir, "add", "README.md")
		runGitCmd(t, repoDir, "commit", "-m", "initial commit")

		status, err := InspectRepo(repoDir)
		if err != nil {
			t.Fatalf("unexpected error inspecting clean repo: %v", err)
		}
		if !status.IsClean {
			t.Errorf("expected repo to be clean, got IsClean=false")
		}
		if status.ModifiedCount != 0 {
			t.Errorf("expected ModifiedCount=0, got %d", status.ModifiedCount)
		}
		if status.CurrentBranch != "main" {
			t.Errorf("expected CurrentBranch='main', got '%s'", status.CurrentBranch)
		}
		if status.AheadCount != 0 || status.BehindCount != 0 {
			t.Errorf("expected 0 ahead and 0 behind, got ahead=%d behind=%d", status.AheadCount, status.BehindCount)
		}
	})

	// 4. Dirty git repository (modified + untracked)
	t.Run("DirtyRepo", func(t *testing.T) {
		repoDir := filepath.Join(tmpDir, "dirtyrepo")
		createTestRepo(t, repoDir, "main")

		file1 := filepath.Join(repoDir, "file1.txt")
		_ = os.WriteFile(file1, []byte("initial\n"), 0644)
		runGitCmd(t, repoDir, "add", "file1.txt")
		runGitCmd(t, repoDir, "commit", "-m", "initial commit")

		// Modify tracked file
		_ = os.WriteFile(file1, []byte("modified\n"), 0644)

		// Create untracked file
		file2 := filepath.Join(repoDir, "untracked.txt")
		_ = os.WriteFile(file2, []byte("new file\n"), 0644)

		status, err := InspectRepo(repoDir)
		if err != nil {
			t.Fatalf("unexpected error inspecting dirty repo: %v", err)
		}
		if status.IsClean {
			t.Errorf("expected repo to be dirty, got IsClean=true")
		}
		if status.ModifiedCount != 2 {
			t.Errorf("expected ModifiedCount=2, got %d", status.ModifiedCount)
		}
	})

	// 5. Ahead / Behind tracking
	t.Run("AheadBehind", func(t *testing.T) {
		bareDir := filepath.Join(tmpDir, "remote.git")
		createBareRepo(t, bareDir)

		cloneDir := filepath.Join(tmpDir, "clonerepo")
		runGitCmd(t, tmpDir, "clone", bareDir, "clonerepo")
		runGitCmd(t, cloneDir, "config", "user.name", "Test User")
		runGitCmd(t, cloneDir, "config", "user.email", "test@example.com")
		runGitCmd(t, cloneDir, "config", "commit.gpgSign", "false")

		// Create and push initial commit
		_ = os.WriteFile(filepath.Join(cloneDir, "init.txt"), []byte("init\n"), 0644)
		runGitCmd(t, cloneDir, "add", "init.txt")
		runGitCmd(t, cloneDir, "commit", "-m", "initial commit")
		runGitCmd(t, cloneDir, "push", "-u", "origin", "main")

		// Create local commit (1 ahead)
		_ = os.WriteFile(filepath.Join(cloneDir, "init.txt"), []byte("local update\n"), 0644)
		runGitCmd(t, cloneDir, "add", "init.txt")
		runGitCmd(t, cloneDir, "commit", "-m", "local commit")

		status, err := InspectRepo(cloneDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status.AheadCount != 1 {
			t.Errorf("expected AheadCount=1, got %d", status.AheadCount)
		}
		if status.BehindCount != 0 {
			t.Errorf("expected BehindCount=0, got %d", status.BehindCount)
		}
	})
}

func TestGetWorkspaceStatus(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup Repo 1: clean
	repo1Dir := filepath.Join(tmpDir, "orbit", "orbit-infra")
	createTestRepo(t, repo1Dir, "main")
	_ = os.WriteFile(filepath.Join(repo1Dir, "infra.txt"), []byte("infra\n"), 0644)
	runGitCmd(t, repo1Dir, "add", "infra.txt")
	runGitCmd(t, repo1Dir, "commit", "-m", "init")

	// Setup Repo 2: dirty
	repo2Dir := filepath.Join(tmpDir, "orbit", "orbit-auth")
	createTestRepo(t, repo2Dir, "main")
	_ = os.WriteFile(filepath.Join(repo2Dir, "auth.txt"), []byte("auth\n"), 0644)
	runGitCmd(t, repo2Dir, "add", "auth.txt")
	runGitCmd(t, repo2Dir, "commit", "-m", "init")
	_ = os.WriteFile(filepath.Join(repo2Dir, "dirty.txt"), []byte("dirty\n"), 0644)

	targets := []manifest.RepoTarget{
		{
			Name:          "orbit-infra",
			Path:          "orbit/orbit-infra",
			DefaultBranch: "main",
		},
		{
			Name:          "orbit-auth",
			Path:          "orbit/orbit-auth",
			DefaultBranch: "main",
		},
		{
			Name:          "orbit-missing",
			Path:          "orbit/orbit-missing",
			DefaultBranch: "main",
		},
	}

	statuses := GetWorkspaceStatus(tmpDir, targets)
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	// Status 1: clean
	if statuses[0].Name != "orbit-infra" || !statuses[0].IsClean || statuses[0].Error != "" {
		t.Errorf("unexpected status 0: %+v", statuses[0])
	}

	// Status 2: dirty
	if statuses[1].Name != "orbit-auth" || statuses[1].IsClean || statuses[1].ModifiedCount != 1 {
		t.Errorf("unexpected status 1: %+v", statuses[1])
	}

	// Status 3: missing
	if statuses[2].Name != "orbit-missing" || statuses[2].Error == "" {
		t.Errorf("expected error for missing repo, got %+v", statuses[2])
	}

	// Test empty targets
	emptyStatuses := GetWorkspaceStatus(tmpDir, nil)
	if len(emptyStatuses) != 0 {
		t.Errorf("expected empty slice for nil targets, got %d", len(emptyStatuses))
	}
}

func TestCloneTarget(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceRoot := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspaceRoot, 0755)

	// Create bare remote repo to clone from
	bareDir := filepath.Join(tmpDir, "remote-core.git")
	createBareRepo(t, bareDir)

	// Seed bare repo with a commit
	seedDir := filepath.Join(tmpDir, "seed")
	createTestRepo(t, seedDir, "main")
	_ = os.WriteFile(filepath.Join(seedDir, "file.txt"), []byte("seed\n"), 0644)
	runGitCmd(t, seedDir, "add", "file.txt")
	runGitCmd(t, seedDir, "commit", "-m", "seed commit")
	runGitCmd(t, seedDir, "remote", "add", "origin", bareDir)
	runGitCmd(t, seedDir, "push", "origin", "main")

	// 1. Fresh clone
	target := manifest.RepoTarget{
		Name:          "orbit-infra",
		Path:          "orbit/orbit-infra",
		RemoteURL:     bareDir,
		DefaultBranch: "main",
	}

	res := CloneTarget(workspaceRoot, target)
	if !res.Success || res.AlreadyExists || res.Error != "" {
		t.Fatalf("expected successful fresh clone, got %+v", res)
	}

	// Verify cloned directory exists and has .git
	if _, err := os.Stat(filepath.Join(workspaceRoot, "orbit", "orbit-infra", ".git")); err != nil {
		t.Fatalf("expected .git directory in cloned path: %v", err)
	}

	// 2. Already exists
	resAgain := CloneTarget(workspaceRoot, target)
	if !resAgain.Success || !resAgain.AlreadyExists {
		t.Fatalf("expected AlreadyExists: true on second clone, got %+v", resAgain)
	}

	// 3. Empty path
	resEmptyPath := CloneTarget(workspaceRoot, manifest.RepoTarget{Name: "empty", Path: ""})
	if resEmptyPath.Success || resEmptyPath.Error == "" {
		t.Errorf("expected error for empty target path, got %+v", resEmptyPath)
	}

	// 4. Empty remote URL
	resEmptyURL := CloneTarget(workspaceRoot, manifest.RepoTarget{Name: "empty-url", Path: "test/empty-url"})
	if resEmptyURL.Success || resEmptyURL.Error == "" {
		t.Errorf("expected error for empty remote URL, got %+v", resEmptyURL)
	}

	// 5. Invalid remote URL
	resInvalidURL := CloneTarget(workspaceRoot, manifest.RepoTarget{
		Name:      "invalid",
		Path:      "test/invalid",
		RemoteURL: filepath.Join(tmpDir, "non-existent-remote.git"),
	})
	if resInvalidURL.Success || resInvalidURL.Error == "" {
		t.Errorf("expected error for invalid remote URL, got %+v", resInvalidURL)
	}
}

func TestCloneTargets(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceRoot := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspaceRoot, 0755)

	// Create 3 bare repos
	targets := make([]manifest.RepoTarget, 3)
	for i := 0; i < 3; i++ {
		bareDir := filepath.Join(tmpDir, "remote", "repo"+string(rune('1'+i))+".git")
		createBareRepo(t, bareDir)

		seedDir := filepath.Join(tmpDir, "seed"+string(rune('1'+i)))
		createTestRepo(t, seedDir, "main")
		_ = os.WriteFile(filepath.Join(seedDir, "file.txt"), []byte("content\n"), 0644)
		runGitCmd(t, seedDir, "add", "file.txt")
		runGitCmd(t, seedDir, "commit", "-m", "init")
		runGitCmd(t, seedDir, "remote", "add", "origin", bareDir)
		runGitCmd(t, seedDir, "push", "origin", "main")

		targets[i] = manifest.RepoTarget{
			Name:      "repo" + string(rune('1'+i)),
			Path:      "repos/repo" + string(rune('1'+i)),
			RemoteURL: bareDir,
		}
	}

	var callbackCount int
	var cbMu sync.Mutex
	callback := func(res CloneResult) {
		cbMu.Lock()
		callbackCount++
		cbMu.Unlock()
	}

	results := CloneTargets(workspaceRoot, targets, 2, callback)
	if len(results) != 3 {
		t.Fatalf("expected 3 clone results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Success || r.Error != "" {
			t.Errorf("target %d failed to clone: %+v", i, r)
		}
	}

	if callbackCount != 3 {
		t.Errorf("expected callback to be called 3 times, got %d", callbackCount)
	}

	// Test empty targets
	emptyResults := CloneTargets(workspaceRoot, nil, 2, nil)
	if len(emptyResults) != 0 {
		t.Errorf("expected empty results for nil targets, got %d", len(emptyResults))
	}
}

func TestSyncRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Non-git directory
	t.Run("NonGitDir", func(t *testing.T) {
		nonGit := filepath.Join(tmpDir, "notgit")
		_ = os.MkdirAll(nonGit, 0755)
		res := SyncRepo(nonGit, "main")
		if res.Success || res.Error == "" {
			t.Errorf("expected error for non-git repo, got %+v", res)
		}
	})

	// Helper to set up a clone with bare remote
	setupRepoWithRemote := func(name string) (string, string) {
		bareDir := filepath.Join(tmpDir, name+"-remote.git")
		createBareRepo(t, bareDir)

		seedDir := filepath.Join(tmpDir, name+"-seed")
		createTestRepo(t, seedDir, "main")
		_ = os.WriteFile(filepath.Join(seedDir, "file.txt"), []byte("v1\n"), 0644)
		runGitCmd(t, seedDir, "add", "file.txt")
		runGitCmd(t, seedDir, "commit", "-m", "v1 commit")
		runGitCmd(t, seedDir, "remote", "add", "origin", bareDir)
		runGitCmd(t, seedDir, "push", "-u", "origin", "main")

		cloneDir := filepath.Join(tmpDir, name+"-clone")
		runGitCmd(t, tmpDir, "clone", bareDir, name+"-clone")
		runGitCmd(t, cloneDir, "config", "user.name", "Test User")
		runGitCmd(t, cloneDir, "config", "user.email", "test@example.com")
		runGitCmd(t, cloneDir, "config", "commit.gpgSign", "false")

		return seedDir, cloneDir
	}

	// 2. Clean repo, already up-to-date
	t.Run("AlreadyUpToDate", func(t *testing.T) {
		_, cloneDir := setupRepoWithRemote("uptodate")
		res := SyncRepo(cloneDir, "main")
		if !res.Success || res.FastForwarded || res.SkippedReason != "" {
			t.Errorf("expected successful sync with no fast-forward, got %+v", res)
		}
	})

	// 3. Clean repo, fast-forward update
	t.Run("FastForward", func(t *testing.T) {
		seedDir, cloneDir := setupRepoWithRemote("ff")

		// Push v2 from seed
		_ = os.WriteFile(filepath.Join(seedDir, "file.txt"), []byte("v2\n"), 0644)
		runGitCmd(t, seedDir, "add", "file.txt")
		runGitCmd(t, seedDir, "commit", "-m", "v2 commit")
		runGitCmd(t, seedDir, "push", "origin", "main")

		res := SyncRepo(cloneDir, "main")
		if !res.Success || !res.FastForwarded {
			t.Errorf("expected successful sync with FastForwarded: true, got %+v", res)
		}

		// Verify content in clone is updated to v2
		content, _ := os.ReadFile(filepath.Join(cloneDir, "file.txt"))
		if string(content) != "v2\n" {
			t.Errorf("expected updated content 'v2\\n', got '%s'", string(content))
		}
	})

	// 4. Dirty working tree (uncommitted changes)
	t.Run("DirtyWorkingTree", func(t *testing.T) {
		_, cloneDir := setupRepoWithRemote("dirty")

		// Modify file without committing
		_ = os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("dirty uncommitted\n"), 0644)

		res := SyncRepo(cloneDir, "main")
		if res.Success || res.SkippedReason != "working tree has uncommitted changes" {
			t.Errorf("expected skipped due to dirty tree, got %+v", res)
		}
	})

	// 5. Non-default branch checked out
	t.Run("NonDefaultBranch", func(t *testing.T) {
		_, cloneDir := setupRepoWithRemote("branch")

		// Checkout feature branch
		runGitCmd(t, cloneDir, "checkout", "-b", "feature/my-branch")

		res := SyncRepo(cloneDir, "main")
		if res.Success || res.SkippedReason == "" {
			t.Errorf("expected skipped due to non-default branch, got %+v", res)
		}
	})

	// 6. Diverged branch
	t.Run("DivergedBranch", func(t *testing.T) {
		seedDir, cloneDir := setupRepoWithRemote("diverged")

		// Push remote commit from seed
		_ = os.WriteFile(filepath.Join(seedDir, "seed-file.txt"), []byte("remote commit\n"), 0644)
		runGitCmd(t, seedDir, "add", "seed-file.txt")
		runGitCmd(t, seedDir, "commit", "-m", "remote commit")
		runGitCmd(t, seedDir, "push", "origin", "main")

		// Create local commit in clone
		_ = os.WriteFile(filepath.Join(cloneDir, "local-file.txt"), []byte("local commit\n"), 0644)
		runGitCmd(t, cloneDir, "add", "local-file.txt")
		runGitCmd(t, cloneDir, "commit", "-m", "local commit")

		res := SyncRepo(cloneDir, "main")
		if res.Success || res.SkippedReason != "branch cannot be fast-forwarded" {
			t.Errorf("expected skipped due to diverged branch, got %+v", res)
		}
	})
}

func TestSyncTargets(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceRoot := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspaceRoot, 0755)

	// Setup Repo 1: up to date
	bareDir1 := filepath.Join(tmpDir, "r1.git")
	createBareRepo(t, bareDir1)
	seed1 := filepath.Join(tmpDir, "seed1")
	createTestRepo(t, seed1, "main")
	_ = os.WriteFile(filepath.Join(seed1, "f.txt"), []byte("1\n"), 0644)
	runGitCmd(t, seed1, "add", "f.txt")
	runGitCmd(t, seed1, "commit", "-m", "init")
	runGitCmd(t, seed1, "remote", "add", "origin", bareDir1)
	runGitCmd(t, seed1, "push", "-u", "origin", "main")

	r1Clone := filepath.Join(workspaceRoot, "orbit", "repo1")
	_ = os.MkdirAll(filepath.Dir(r1Clone), 0755)
	runGitCmd(t, filepath.Dir(r1Clone), "clone", bareDir1, "repo1")

	// Setup Repo 2: missing
	targets := []manifest.RepoTarget{
		{
			Name:          "repo1",
			Path:          "orbit/repo1",
			DefaultBranch: "main",
		},
		{
			Name:          "repo-missing",
			Path:          "orbit/repo-missing",
			DefaultBranch: "main",
		},
	}

	results := SyncTargets(workspaceRoot, targets, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if !results[0].Success || results[0].Name != "repo1" {
		t.Errorf("expected successful sync for repo1, got %+v", results[0])
	}

	if results[1].Success || results[1].Error != "repository not cloned" {
		t.Errorf("expected error for missing repo, got %+v", results[1])
	}

	// Test empty targets
	emptyResults := SyncTargets(workspaceRoot, nil, 2)
	if len(emptyResults) != 0 {
		t.Errorf("expected empty results for nil targets, got %d", len(emptyResults))
	}
}
