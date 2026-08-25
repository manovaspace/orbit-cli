package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCmdWithLocalRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a local bare git repo as source
	bareRepoDir := filepath.Join(tmpDir, "source.git")
	_ = os.MkdirAll(bareRepoDir, 0755)
	_ = exec.Command("git", "init", "--bare", bareRepoDir).Run()

	manifestFile := filepath.Join(tmpDir, "workspace.yaml")
	manifestContent := `
version: "1"
workspace: "test-space"
groups:
  orbit:
    path: "orbit"
    repositories:
      - name: "sample-repo"
        path: "orbit/sample-repo"
        remote_url: "` + bareRepoDir + `"
        required: true
`
	if err := os.WriteFile(manifestFile, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"init", "core", "--manifest", manifestFile, "--skip-hooks"})

	// Set MANOVA_ROOT to tmpDir
	t.Setenv("MANOVA_ROOT", tmpDir)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "sample-repo") {
		t.Errorf("expected output to mention sample-repo, got: %s", output)
	}

	// Verify repo directory exists
	targetRepoDir := filepath.Join(tmpDir, "orbit", "sample-repo", ".git")
	if _, err := os.Stat(targetRepoDir); err != nil {
		t.Errorf("expected target repo .git directory to exist at %s: %v", targetRepoDir, err)
	}
}

func TestInitBootstrapFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"init", "--bootstrap"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init --bootstrap failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Manova developer environment initialized") {
		t.Errorf("expected bootstrap success message, got: %s", output)
	}
}
