package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	migratedomain "github.com/manovaspace/orbit-cli/pkg/migrate/domain"
	"github.com/manovaspace/orbit-cli/pkg/migrate/infrastructure/statefile"
)

func TestMigrateCmd_AllUpToDate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out := &bytes.Buffer{}
	cmd := newMigrateCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected migrate command to succeed, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Workspace migrations") {
		t.Errorf("output missing title:\n%s", output)
	}
	if !strings.Contains(output, "No migrations found.") {
		t.Errorf("output missing empty-result message:\n%s", output)
	}
	if !strings.Contains(output, "0 applied") {
		t.Errorf("output missing applied count:\n%s", output)
	}
}

func TestMigrateStatusCmd_TableStructure(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out := &bytes.Buffer{}
	cmd := newMigrateStatusCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected migrate status to succeed, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Workspace migrations") {
		t.Errorf("output missing title:\n%s", output)
	}
	if !strings.Contains(output, "No migrations found.") {
		t.Errorf("output missing empty-result message:\n%s", output)
	}
	if !strings.Contains(output, "0 applied") || !strings.Contains(output, "0 pending") {
		t.Errorf("output missing summary footer counts:\n%s", output)
	}
}

func TestMigrateStatusCmd_WithAppliedState(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	appliedTime := time.Date(2026, 8, 31, 14, 30, 0, 0, time.UTC)
	state := &migratedomain.MigrationState{
		Version: migratedomain.DefaultStateVersion,
		Applied: []migratedomain.AppliedMigrationRecord{{
			ID:          "001_workspace_init",
			Description: "Initialize workspace directory tree",
			AppliedAt:   appliedTime,
		}},
	}
	if err := statefile.New().Save(filepath.Join(tmpDir, ".orbit", "migrations.json"), state); err != nil {
		t.Fatalf("failed to save migration state: %v", err)
	}

	out := &bytes.Buffer{}
	cmd := newMigrateStatusCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected migrate status to succeed, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Workspace migrations") {
		t.Errorf("output missing title:\n%s", output)
	}
	if !strings.Contains(output, "migrations.json") {
		t.Errorf("output missing state file path:\n%s", output)
	}
	if !strings.Contains(output, "No migrations found.") {
		t.Errorf("output missing empty-result message:\n%s", output)
	}
}

func TestMigrateStatusCmd_InvalidStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	stateDir := filepath.Join(tmpDir, ".orbit")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create .orbit dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "migrations.json"), []byte("{corrupt-json"), 0644); err != nil {
		t.Fatalf("failed to write corrupt state file: %v", err)
	}

	cmd := newMigrateStatusCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error with corrupt state file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to load migration state") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestMigrateJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out, err := runRootForTest(t, "migrate", "--json")
	if err != nil {
		t.Fatalf("migrate --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		WorkspaceRoot string `json:"workspaceRoot"`
		Applied       int    `json:"applied"`
		Results       []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.WorkspaceRoot != tmpDir {
		t.Errorf("workspaceRoot = %q, want %q", data.WorkspaceRoot, tmpDir)
	}
	if data.Applied != 0 {
		t.Errorf("applied = %d, want 0", data.Applied)
	}
}

func TestMigrateStatusJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	out, err := runRootForTest(t, "migrate", "status", "--json")
	if err != nil {
		t.Fatalf("migrate status --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		StatePath string `json:"statePath"`
		Applied   int    `json:"applied"`
		Pending   int    `json:"pending"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.StatePath != filepath.Join(tmpDir, ".orbit", "migrations.json") {
		t.Errorf("statePath = %q", data.StatePath)
	}
	if data.Applied != 0 || data.Pending != 0 {
		t.Errorf("counts = %d/%d", data.Applied, data.Pending)
	}
}
