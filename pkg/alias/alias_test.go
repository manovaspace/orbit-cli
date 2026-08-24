package alias

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsCommandTaken(t *testing.T) {
	// Standard Unix command that should always exist
	taken, reason := IsCommandTaken("ls")
	if !taken {
		t.Errorf("expected 'ls' to be taken, got false")
	}
	if reason == "" {
		t.Errorf("expected non-empty reason for taken command")
	}

	// High-entropy unlikely command name
	unlikelyCmd := "unlikely_manova_fake_cmd_xyz_123"
	taken, _ = IsCommandTaken(unlikelyCmd)
	if taken {
		t.Errorf("expected %q not to be taken, got true", unlikelyCmd)
	}
}

func TestAddShellAlias(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("SHELL", "/bin/bash")

	rcFile := filepath.Join(tmpDir, ".bashrc")
	_ = os.WriteFile(rcFile, []byte("# Existing bashrc\n"), 0644)

	targetFile, err := AddShellAlias("m", "manova")
	if err != nil {
		t.Fatalf("AddShellAlias failed: %v", err)
	}
	if targetFile != rcFile {
		t.Errorf("expected targetFile %q, got %q", rcFile, targetFile)
	}

	content, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatalf("failed to read rcFile: %v", err)
	}

	if !strings.Contains(string(content), `alias m="manova"`) {
		t.Errorf("expected alias line in file, got: %s", string(content))
	}

	// Test idempotency (should not duplicate entry)
	_, err = AddShellAlias("m", "manova")
	if err != nil {
		t.Fatalf("second AddShellAlias call failed: %v", err)
	}

	contentSecond, _ := os.ReadFile(rcFile)
	count := strings.Count(string(contentSecond), `alias m="manova"`)
	if count != 1 {
		t.Errorf("expected exactly 1 instance of alias, found %d", count)
	}
}
