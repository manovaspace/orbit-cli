package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusCmdWithSampleManifest(t *testing.T) {
	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "workspace.yaml")

	manifestContent := `
version: "1"
workspace: "test-space"
groups:
  orbit:
    path: "orbit"
    repositories:
      - name: "test-repo"
        path: "test-repo"
`
	if err := os.WriteFile(manifestFile, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "--manifest", manifestFile})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("status command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "REPOSITORY") || !strings.Contains(output, "test-repo") {
		t.Errorf("expected table header and repo name, got: %s", output)
	}
}
