package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncCmdWithManifest(t *testing.T) {
	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "workspace.yaml")

	manifestContent := `
version: "1"
workspace: "test-space"
groups:
  orbit:
    path: "orbit"
    repositories:
      - name: "test-sync-repo"
        path: "test-sync-repo"
`
	if err := os.WriteFile(manifestFile, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	t.Setenv("MANOVA_ROOT", tmpDir)

	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"sync", "--manifest", manifestFile})

	// Execution should handle uncloned repo gracefully
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "test-sync-repo") {
		t.Errorf("expected output to mention test-sync-repo, got: %s", output)
	}
}
