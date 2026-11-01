package statefile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/migrate/domain"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	state, err := New().Load(filepath.Join(t.TempDir(), ".orbit", "migrations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != domain.DefaultStateVersion {
		t.Errorf("version = %q", state.Version)
	}
	if len(state.Applied) != 0 {
		t.Errorf("applied = %+v", state.Applied)
	}
}

func TestSaveReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".orbit", "migrations.json")
	store := New()
	at := time.Date(2026, 8, 31, 14, 30, 0, 0, time.UTC)
	state := &domain.MigrationState{
		Version: domain.DefaultStateVersion,
		Applied: []domain.AppliedMigrationRecord{{ID: "m1", Description: "one", AppliedAt: at}},
	}
	if err := store.Save(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.IsApplied("m1") || len(loaded.Applied) != 1 {
		t.Fatalf("state = %+v", loaded)
	}
	if !loaded.Applied[0].AppliedAt.Equal(at) {
		t.Errorf("applied at = %v", loaded.Applied[0].AppliedAt)
	}
}

func TestLoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrations.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Load(path); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSaveUncreatablePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New().Save(filepath.Join(file, "nested", "state.json"), &domain.MigrationState{}); err == nil {
		t.Fatal("expected an error")
	}
}
