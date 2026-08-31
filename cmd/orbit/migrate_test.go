package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/migrate"
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
	if !strings.Contains(output, "Orbit Workspace Migrations") {
		t.Errorf("output missing title:\n%s", output)
	}
	if !strings.Contains(output, "All workspace migrations are up to date") {
		t.Errorf("output missing up-to-date message:\n%s", output)
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
	if !strings.Contains(output, "Orbit Workspace Migration Status") {
		t.Errorf("output missing title:\n%s", output)
	}
	if !strings.Contains(output, "MIGRATION ID") {
		t.Errorf("output missing MIGRATION ID column header:\n%s", output)
	}
	if !strings.Contains(output, "DESCRIPTION") {
		t.Errorf("output missing DESCRIPTION column header:\n%s", output)
	}
	if !strings.Contains(output, "STATUS") {
		t.Errorf("output missing STATUS column header:\n%s", output)
	}
	if !strings.Contains(output, "0 applied") || !strings.Contains(output, "0 pending") {
		t.Errorf("output missing summary footer counts:\n%s", output)
	}
}

func TestMigrateStatusCmd_WithAppliedAndPendingMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	// Create state file with an applied migration record
	stateDir := filepath.Join(tmpDir, ".orbit")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create .orbit directory: %v", err)
	}

	appliedTime := time.Date(2026, 8, 31, 14, 30, 0, 0, time.UTC)
	state := &migrate.MigrationState{
		Version: migrate.DefaultStateVersion,
		Applied: []migrate.AppliedMigrationRecord{
			{
				ID:          "001_workspace_init",
				Description: "Initialize workspace directory tree",
				AppliedAt:   appliedTime,
			},
		},
	}

	engine := migrate.NewEngine(tmpDir, "")
	if err := engine.SaveState(state); err != nil {
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
	if !strings.Contains(output, "Orbit Workspace Migration Status") {
		t.Errorf("output missing title:\n%s", output)
	}
	if !strings.Contains(output, "MIGRATION ID") || !strings.Contains(output, "DESCRIPTION") || !strings.Contains(output, "STATUS") {
		t.Errorf("output missing headers:\n%s", output)
	}
	// Check state file path mentioned
	if !strings.Contains(output, "migrations.json") {
		t.Errorf("output missing state file path:\n%s", output)
	}
}

func TestMigrateStatusCmd_InvalidStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", tmpDir)

	stateDir := filepath.Join(tmpDir, ".orbit")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create .orbit dir: %v", err)
	}
	// Write corrupt JSON
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
