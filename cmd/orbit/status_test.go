package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusTableOutputStructure(t *testing.T) {
	// Verify that table output contains aligned columns and headers
	out := &bytes.Buffer{}
	cmd := newStatusCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all"})

	// Run status against the current repo/workspace
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error running status: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "REPOSITORY") || !strings.Contains(output, "PATH") || !strings.Contains(output, "BRANCH") {
		t.Errorf("status output missing expected headers:\n%s", output)
	}
	if !strings.Contains(output, "WORKING TREE") {
		t.Errorf("status output missing WORKING TREE header:\n%s", output)
	}
	if !strings.Contains(output, "SYNC") {
		t.Errorf("status output missing SYNC header:\n%s", output)
	}
}

func TestStatusTableWithVariousRepoStates(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create a clean repo
	cleanRepo := filepath.Join(tmpDir, "repo-clean")
	if err := os.MkdirAll(cleanRepo, 0755); err != nil {
		t.Fatalf("failed to create repo-clean dir: %v", err)
	}
	exec.Command("git", "-C", cleanRepo, "init", "-b", "main").Run()
	exec.Command("git", "-C", cleanRepo, "config", "user.name", "Test").Run()
	exec.Command("git", "-C", cleanRepo, "config", "user.email", "test@example.com").Run()
	os.WriteFile(filepath.Join(cleanRepo, "file.txt"), []byte("clean"), 0644)
	exec.Command("git", "-C", cleanRepo, "add", "file.txt").Run()
	exec.Command("git", "-C", cleanRepo, "commit", "-m", "initial commit").Run()

	// 2. Create a dirty repo
	dirtyRepo := filepath.Join(tmpDir, "repo-dirty")
	if err := os.MkdirAll(dirtyRepo, 0755); err != nil {
		t.Fatalf("failed to create repo-dirty dir: %v", err)
	}
	exec.Command("git", "-C", dirtyRepo, "init", "-b", "feature").Run()
	exec.Command("git", "-C", dirtyRepo, "config", "user.name", "Test").Run()
	exec.Command("git", "-C", dirtyRepo, "config", "user.email", "test@example.com").Run()
	os.WriteFile(filepath.Join(dirtyRepo, "file.txt"), []byte("initial"), 0644)
	exec.Command("git", "-C", dirtyRepo, "add", "file.txt").Run()
	exec.Command("git", "-C", dirtyRepo, "commit", "-m", "initial commit").Run()
	os.WriteFile(filepath.Join(dirtyRepo, "file.txt"), []byte("modified content"), 0644)

	// 3. Create a gitless directory
	gitlessRepo := filepath.Join(tmpDir, "repo-gitless")
	if err := os.MkdirAll(gitlessRepo, 0755); err != nil {
		t.Fatalf("failed to create repo-gitless dir: %v", err)
	}

	// 4. Create workspace.yaml
	manifestContent := `version: "1"
workspace: "test-workspace"
remotes:
  forgejo: "ssh://git@git.dev.manova.space/manova"
groups:
  testgroup:
    path: ""
    defaults:
      remote: "forgejo"
    repositories:
      - name: "repo-clean"
        path: "repo-clean"
      - name: "repo-dirty"
        path: "repo-dirty"
      - name: "repo-missing"
        path: "repo-missing"
      - name: "repo-gitless"
        path: "repo-gitless"
`
	manifestPath := filepath.Join(tmpDir, "workspace.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write workspace.yaml: %v", err)
	}

	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out := &bytes.Buffer{}
	cmd := newStatusCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all", "--manifest", manifestPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command failed: %v", err)
	}

	output := out.String()

	// Check table columns and content
	if !strings.Contains(output, "repo-clean") {
		t.Errorf("expected repo-clean in output:\n%s", output)
	}
	if !strings.Contains(output, "repo-dirty") {
		t.Errorf("expected repo-dirty in output:\n%s", output)
	}
	if !strings.Contains(output, "repo-missing") {
		t.Errorf("expected repo-missing in output:\n%s", output)
	}
	if !strings.Contains(output, "repo-gitless") {
		t.Errorf("expected repo-gitless in output:\n%s", output)
	}
	if !strings.Contains(output, "not cloned") {
		t.Errorf("expected 'not cloned' indicator in output:\n%s", output)
	}
	if !strings.Contains(output, "gitless") {
		t.Errorf("expected 'gitless' indicator in output:\n%s", output)
	}
	if !strings.Contains(output, "clean") {
		t.Errorf("expected 'clean' indicator in output:\n%s", output)
	}
	if !strings.Contains(output, "dirty") && !strings.Contains(output, "modified") {
		t.Errorf("expected 'dirty' or 'modified' indicator in output:\n%s", output)
	}
	if !strings.Contains(output, "Total: 4") {
		t.Errorf("expected 'Total: 4' summary footer in output:\n%s", output)
	}
}

func TestStatusScopeEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	manifestContent := `version: "1"
workspace: "test-workspace"
remotes:
  forgejo: "ssh://git@git.dev.manova.space/manova"
groups:
  testgroup:
    path: ""
    repositories:
      - name: "myrepo"
`
	manifestPath := filepath.Join(tmpDir, "workspace.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write workspace.yaml: %v", err)
	}

	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out := &bytes.Buffer{}
	cmd := newStatusCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"nonexistent-scope", "--manifest", manifestPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no command error for empty scope, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, `No repositories found for scope "nonexistent-scope"`) {
		t.Errorf("expected empty scope message, got:\n%s", output)
	}
}

func TestStatusMissingManifest(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out := &bytes.Buffer{}
	cmd := newStatusCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all", "--manifest", filepath.Join(tmpDir, "nonexistent.yaml")})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent manifest, got nil")
	}
	if !strings.Contains(err.Error(), "manifest file not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStatusTableSyncStates(t *testing.T) {
	tmpDir := t.TempDir()

	// Upstream bare repository
	upstream := filepath.Join(tmpDir, "upstream.git")
	exec.Command("git", "init", "--bare", upstream).Run()

	// Clone to local repo
	localRepo := filepath.Join(tmpDir, "repo-ahead")
	exec.Command("git", "clone", upstream, localRepo).Run()
	exec.Command("git", "-C", localRepo, "config", "user.name", "Test").Run()
	exec.Command("git", "-C", localRepo, "config", "user.email", "test@example.com").Run()
	exec.Command("git", "-C", localRepo, "checkout", "-b", "main").Run()
	os.WriteFile(filepath.Join(localRepo, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "-C", localRepo, "add", "file.txt").Run()
	exec.Command("git", "-C", localRepo, "commit", "-m", "v1").Run()
	exec.Command("git", "-C", localRepo, "push", "-u", "origin", "main").Run()

	// Add a local commit (now 1 ahead)
	os.WriteFile(filepath.Join(localRepo, "file.txt"), []byte("v2"), 0644)
	exec.Command("git", "-C", localRepo, "add", "file.txt").Run()
	exec.Command("git", "-C", localRepo, "commit", "-m", "v2").Run()

	manifestContent := `version: "1"
workspace: "test-workspace"
remotes:
  forgejo: "ssh://git@git.dev.manova.space/manova"
groups:
  testgroup:
    path: ""
    repositories:
      - name: "repo-ahead"
        path: "repo-ahead"
`
	manifestPath := filepath.Join(tmpDir, "workspace.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write workspace.yaml: %v", err)
	}

	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out := &bytes.Buffer{}
	cmd := newStatusCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all", "--manifest", manifestPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "↑1 ahead") {
		t.Errorf("expected '↑1 ahead' in output:\n%s", output)
	}
}


