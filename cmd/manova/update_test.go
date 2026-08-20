package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdateCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("MANOVA_ROOT", tmpDir)

	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"update", "--skip-selfupdate"})

	// Execute unified update
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Unified Workspace Update") {
		t.Errorf("expected header in update output, got: %s", output)
	}
	if !strings.Contains(output, "Workspace Migrations") {
		t.Errorf("expected migrations step in output, got: %s", output)
	}
	if !strings.Contains(output, "Environment Validation") {
		t.Errorf("expected env validation step in output, got: %s", output)
	}
}
