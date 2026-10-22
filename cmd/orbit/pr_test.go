package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code.gitea.io/sdk/gitea"
)

func setupTestGitRepo(t *testing.T, owner, repoName string) (localDir, originDir string) {
	t.Helper()
	tmpDir := t.TempDir()

	originDir = filepath.Join(tmpDir, owner, repoName+".git")
	if err := os.MkdirAll(filepath.Dir(originDir), 0755); err != nil {
		t.Fatalf("failed to mkdir origin parent: %v", err)
	}
	cmd := exec.Command("git", "init", "--bare", originDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init bare origin: %s (%v)", out, err)
	}

	localDir = filepath.Join(tmpDir, "local")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatalf("failed to mkdir local: %v", err)
	}

	runGitCmd := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", localDir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %s (%v)", strings.Join(args, " "), out, err)
		}
	}

	runGitCmd("init")
	runGitCmd("config", "user.name", "Orbit Test")
	runGitCmd("config", "user.email", "test@manova.space")
	runGitCmd("config", "init.defaultBranch", "main")
	runGitCmd("config", "core.hooksPath", "/dev/null")
	runGitCmd("checkout", "-b", "main")

	runGitCmd("remote", "add", "origin", originDir)

	readme := filepath.Join(localDir, "README.md")
	if err := os.WriteFile(readme, []byte("# Initial Commit\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runGitCmd("add", "README.md")
	runGitCmd("commit", "-m", "chore: initial commit")
	runGitCmd("push", "-u", "origin", "main")

	return localDir, originDir
}

func TestPRHelp(t *testing.T) {
	subcommands := []string{"", "create", "merge", "status", "deliver"}
	for _, sub := range subcommands {
		t.Run("Help_"+sub, func(t *testing.T) {
			buf := new(bytes.Buffer)
			cmd := newRootCmd()
			cmd.SetOut(buf)
			cmd.SetErr(buf)

			args := []string{"pr"}
			if sub != "" {
				args = append(args, sub)
			}
			args = append(args, "--help")
			cmd.SetArgs(args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error executing pr %s --help: %v", sub, err)
			}

			out := buf.String()
			if !strings.Contains(out, "Usage:") {
				t.Errorf("expected usage output, got: %s", out)
			}
		})
	}
}

func TestPRStatusWithMockServer(t *testing.T) {
	owner := "manova"
	repo := "toolkit"
	localDir, _ := setupTestGitRepo(t, owner, repo)

	now := time.Now()
	mockPR := &gitea.PullRequest{
		Index:   101,
		Title:   "feat(api): add auth endpoint",
		HTMLURL: "https://git.internal.manova.space/manova/toolkit/pulls/101",
		Poster:  &gitea.User{UserName: "testuser"},
		Head:    &gitea.PRBranchInfo{Name: "feat/auth"},
		Base:    &gitea.PRBranchInfo{Name: "main"},
		State:   gitea.StateOpen,
		Created: &now,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/manova/toolkit/pulls" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*gitea.PullRequest{mockPR})
			return
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer ts.Close()

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"pr", "status",
		"--dir", localDir,
		"--url", ts.URL,
		"--token", "test-token",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("pr status failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "#101") {
		t.Errorf("expected output to contain '#101', got: %s", out)
	}
	if !strings.Contains(out, "feat(api): add auth") {
		t.Errorf("expected output to contain PR title prefix, got: %s", out)
	}
	if !strings.Contains(out, "feat/auth -> main") {
		t.Errorf("expected output to contain branch mapping, got: %s", out)
	}
	if !strings.Contains(out, "testuser") {
		t.Errorf("expected output to contain author 'testuser', got: %s", out)
	}
}

func TestPRStatusEmpty(t *testing.T) {
	owner := "manova"
	repo := "toolkit"
	localDir, _ := setupTestGitRepo(t, owner, repo)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/manova/toolkit/pulls" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*gitea.PullRequest{})
			return
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer ts.Close()

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"pr", "status",
		"--dir", localDir,
		"--url", ts.URL,
		"--token", "test-token",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("pr status failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "No open pull requests") {
		t.Errorf("expected 'No open pull requests', got: %s", out)
	}
}

func TestPRCreate(t *testing.T) {
	owner := "manova"
	repo := "handbook"
	localDir, _ := setupTestGitRepo(t, owner, repo)

	// Switch to feature branch and make commit
	c := exec.Command("git", "-C", localDir, "checkout", "-b", "feat/my-change")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b failed: %s (%v)", out, err)
	}
	f := filepath.Join(localDir, "change.txt")
	_ = os.WriteFile(f, []byte("change\n"), 0644)
	c = exec.Command("git", "-C", localDir, "add", "change.txt")
	_ = c.Run()
	c = exec.Command("git", "-C", localDir, "commit", "-m", "feat(guide): improve onboarding guide")
	_ = c.Run()

	createdPR := &gitea.PullRequest{
		Index:   77,
		Title:   "feat(guide): improve onboarding guide",
		HTMLURL: "https://git.internal.manova.space/manova/handbook/pulls/77",
		Base:    &gitea.PRBranchInfo{Name: "main"},
		Head:    &gitea.PRBranchInfo{Name: "feat/my-change"},
		State:   gitea.StateOpen,
	}

	var createCalled, mergeCalled bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/manova/handbook/pulls":
			createCalled = true
			var opt gitea.CreatePullRequestOption
			_ = json.NewDecoder(r.Body).Decode(&opt)
			if opt.Title != "feat(guide): improve onboarding guide" {
				t.Errorf("unexpected PR title: %q", opt.Title)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(createdPR)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/manova/handbook/pulls/77/merge":
			mergeCalled = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "merged"})

		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	t.Run("CreateWithDerivedTitleAndMerge", func(t *testing.T) {
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{
			"pr", "create",
			"--dir", localDir,
			"--url", ts.URL,
			"--token", "test-token",
			"--merge",
			"--method", "merge",
		})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("pr create failed: %v", err)
		}

		if !createCalled {
			t.Error("expected CreatePR API to be called")
		}
		if !mergeCalled {
			t.Error("expected MergePR API to be called because --merge was set")
		}

		out := buf.String()
		if !strings.Contains(out, "Pull request created:") {
			t.Errorf("expected PR creation notice, got: %s", out)
		}
		if !strings.Contains(out, "Pull request #77 merged") {
			t.Errorf("expected PR merged notice, got: %s", out)
		}
	})
}

func TestPRMerge(t *testing.T) {
	owner := "clients"
	repo := "pulse"
	localDir, _ := setupTestGitRepo(t, owner, repo)

	c := exec.Command("git", "-C", localDir, "checkout", "-b", "feat/pulse-fix")
	_ = c.Run()

	var mergeCalledWithIndex int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/clients/pulse/pulls":
			openPR := &gitea.PullRequest{
				Index: 44,
				Head:  &gitea.PRBranchInfo{Name: "feat/pulse-fix"},
				Base:  &gitea.PRBranchInfo{Name: "main"},
				State: gitea.StateOpen,
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*gitea.PullRequest{openPR})

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/repos/clients/pulse/pulls/"):
			var idx int64
			fmt.Sscanf(r.URL.Path, "/api/v1/repos/clients/pulse/pulls/%d/merge", &idx)
			mergeCalledWithIndex = idx
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "merged"})

		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	t.Run("MergeExplicitIndex", func(t *testing.T) {
		mergeCalledWithIndex = 0
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{
			"pr", "merge", "55",
			"--dir", localDir,
			"--url", ts.URL,
			"--token", "test-token",
			"--method", "squash",
		})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("pr merge 55 failed: %v", err)
		}
		if mergeCalledWithIndex != 55 {
			t.Errorf("expected merged index 55, got %d", mergeCalledWithIndex)
		}
		if !strings.Contains(buf.String(), "Pull request #55 merged using method \"squash\"") {
			t.Errorf("unexpected output: %s", buf.String())
		}
	})

	t.Run("MergeAutoDetectBranch", func(t *testing.T) {
		mergeCalledWithIndex = 0
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{
			"pr", "merge",
			"--dir", localDir,
			"--url", ts.URL,
			"--token", "test-token",
		})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("pr merge auto-detect failed: %v", err)
		}
		if mergeCalledWithIndex != 44 {
			t.Errorf("expected auto-detected merged index 44, got %d", mergeCalledWithIndex)
		}
	})
}

func TestPRDeliverEndToEnd(t *testing.T) {
	owner := "manova"
	repo := "delivery-repo"
	localDir, originDir := setupTestGitRepo(t, owner, repo)

	// Checkout topic branch
	c := exec.Command("git", "-C", localDir, "checkout", "-b", "feat/shiny-feature")
	_ = c.Run()

	featureFile := filepath.Join(localDir, "feature.go")
	_ = os.WriteFile(featureFile, []byte("package feature\n"), 0644)
	c = exec.Command("git", "-C", localDir, "add", "feature.go")
	_ = c.Run()
	c = exec.Command("git", "-C", localDir, "commit", "-m", "feat(core): implement shiny feature")
	_ = c.Run()

	createdPR := &gitea.PullRequest{
		Index:   88,
		Title:   "feat(core): implement shiny feature",
		HTMLURL: "https://git.internal.manova.space/manova/delivery-repo/pulls/88",
		Base:    &gitea.PRBranchInfo{Name: "main"},
		Head:    &gitea.PRBranchInfo{Name: "feat/shiny-feature"},
		State:   gitea.StateOpen,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/manova/delivery-repo/pulls":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(createdPR)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/manova/delivery-repo/pulls/88/merge":
			// On remote git server, merging the PR advances main on origin
			// Simulate this in our bare origin repo: push feat/shiny-feature to origin main
			pushCmd := exec.Command("git", "-C", localDir, "push", originDir, "feat/shiny-feature:main")
			if pushOut, pushErr := pushCmd.CombinedOutput(); pushErr != nil {
				t.Errorf("failed to simulate merge on origin: %s (%v)", pushOut, pushErr)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "merged"})

		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"pr", "deliver",
		"--dir", localDir,
		"--url", ts.URL,
		"--token", "test-token",
		"--base", "main",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("pr deliver failed: %v\nOutput: %s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Successfully delivered feat/shiny-feature into main!") {
		t.Errorf("expected success message, got: %s", out)
	}

	// Verify we are back on main branch
	branchCmd := exec.Command("git", "-C", localDir, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := branchCmd.Output()
	if err != nil {
		t.Fatalf("failed to get active branch: %v", err)
	}
	if strings.TrimSpace(string(branchOut)) != "main" {
		t.Errorf("expected active branch to be 'main', got %q", strings.TrimSpace(string(branchOut)))
	}

	// Verify local branch feat/shiny-feature was pruned
	checkBranchCmd := exec.Command("git", "-C", localDir, "branch", "--list", "feat/shiny-feature")
	checkBranchOut, _ := checkBranchCmd.Output()
	if strings.TrimSpace(string(checkBranchOut)) != "" {
		t.Errorf("expected local branch feat/shiny-feature to be deleted, got: %s", checkBranchOut)
	}
}

func TestPRDeliverValidationErrors(t *testing.T) {
	owner := "manova"
	repo := "errors-repo"
	localDir, _ := setupTestGitRepo(t, owner, repo)

	t.Run("CannotDeliverFromBaseBranch", func(t *testing.T) {
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{
			"pr", "deliver",
			"--dir", localDir,
			"--token", "test-token",
			"--base", "main",
		})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error delivering from main branch, got nil")
		}
		if !strings.Contains(err.Error(), "cannot deliver from \"main\" branch") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("CannotDeliverWithDirtyTree", func(t *testing.T) {
		// Checkout topic branch
		c := exec.Command("git", "-C", localDir, "checkout", "-b", "feat/dirty-branch")
		_ = c.Run()

		// Write uncommitted file
		_ = os.WriteFile(filepath.Join(localDir, "uncommitted.txt"), []byte("dirty"), 0644)

		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{
			"pr", "deliver",
			"--dir", localDir,
			"--token", "test-token",
		})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error with dirty tree, got nil")
		}
		if !strings.Contains(err.Error(), "working tree has uncommitted changes") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
