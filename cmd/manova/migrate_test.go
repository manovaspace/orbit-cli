package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMigrateAndStatus(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("MANOVA_ROOT", tmpDir)

	// 1. Check initial migrate status (should show pending)
	statusBuf := new(bytes.Buffer)
	statusCmd := newRootCmd()
	statusCmd.SetOut(statusBuf)
	statusCmd.SetErr(statusBuf)
	statusCmd.SetArgs([]string{"migrate", "status"})

	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("migrate status failed: %v", err)
	}

	statusOut := statusBuf.String()
	if !strings.Contains(statusOut, "001_ensure_workspace_dirs") {
		t.Errorf("expected migration 001 in status output, got: %s", statusOut)
	}

	// 2. Run migrations
	runBuf := new(bytes.Buffer)
	runCmd := newRootCmd()
	runCmd.SetOut(runBuf)
	runCmd.SetErr(runBuf)
	runCmd.SetArgs([]string{"migrate"})

	if err := runCmd.Execute(); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}

	runOut := runBuf.String()
	if !strings.Contains(runOut, "Applied") {
		t.Errorf("expected 'Applied' in migrate run output, got: %s", runOut)
	}

	// 3. Re-run migrate (should show 0 pending)
	rerunBuf := new(bytes.Buffer)
	rerunCmd := newRootCmd()
	rerunCmd.SetOut(rerunBuf)
	rerunCmd.SetErr(rerunBuf)
	rerunCmd.SetArgs([]string{"migrate"})

	if err := rerunCmd.Execute(); err != nil {
		t.Fatalf("migrate rerun failed: %v", err)
	}

	rerunOut := rerunBuf.String()
	if !strings.Contains(rerunOut, "0 pending") && !strings.Contains(rerunOut, "up to date") {
		t.Errorf("expected 0 pending in rerun output, got: %s", rerunOut)
	}
}
