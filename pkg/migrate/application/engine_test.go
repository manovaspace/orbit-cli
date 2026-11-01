package application

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/migrate/domain"
)

// fakeStore keeps the migration state in memory.
type fakeStore struct {
	state *domain.MigrationState
	load  error
	save  error
	saves int
}

func (s *fakeStore) Load(string) (*domain.MigrationState, error) {
	if s.load != nil {
		return nil, s.load
	}
	if s.state == nil {
		return &domain.MigrationState{}, nil
	}
	copied := *s.state
	copied.Applied = append([]domain.AppliedMigrationRecord(nil), s.state.Applied...)
	return &copied, nil
}

func (s *fakeStore) Save(_ string, state *domain.MigrationState) error {
	if s.save != nil {
		return s.save
	}
	copied := *state
	copied.Applied = append([]domain.AppliedMigrationRecord(nil), state.Applied...)
	s.state = &copied
	s.saves++
	return nil
}

func newEngine(store *fakeStore) *Engine {
	return NewEngine("/ws", "", store)
}

func TestNewEngineDefaultsStatePath(t *testing.T) {
	engine := NewEngine("/ws", "", &fakeStore{})
	if engine.StatePath != "/ws/.orbit/migrations.json" {
		t.Errorf("state path = %q", engine.StatePath)
	}
	engine = NewEngine("/ws", "/custom/state.json", &fakeStore{})
	if engine.StatePath != "/custom/state.json" {
		t.Errorf("state path = %q", engine.StatePath)
	}
}

func TestMigrationStatePersistence(t *testing.T) {
	store := &fakeStore{}
	engine := newEngine(store)
	migrations := []domain.Migration{
		{ID: "001_install_hooks", Description: "Install git hooks", Run: func(string) error { return nil }},
		{ID: "002_symlink_rules", Description: "Symlink Cursor rules", Run: func(string) error { return nil }},
	}
	applied, err := engine.Apply(migrations)
	if err != nil || len(applied) != 2 {
		t.Fatalf("expected 2 applied migrations, got %d (err: %v)", len(applied), err)
	}
	second, err := engine.Apply(migrations)
	if err != nil {
		t.Fatalf("unexpected error on second apply: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("expected 0 new migrations, got %d", len(second))
	}
}

func TestEngineLoadNormalisesDefaults(t *testing.T) {
	engine := newEngine(&fakeStore{state: &domain.MigrationState{}})
	state, err := engine.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != domain.DefaultStateVersion {
		t.Errorf("version = %q", state.Version)
	}
	if state.Applied == nil {
		t.Error("applied is nil")
	}
}

func TestSaveNilStateErrors(t *testing.T) {
	if err := newEngine(&fakeStore{}).SaveState(nil); err == nil {
		t.Fatal("expected an error")
	}
}

func TestEnginePending(t *testing.T) {
	engine := newEngine(&fakeStore{})
	all := []domain.Migration{
		{ID: "m1", Description: "Migration 1"},
		{ID: "m2", Description: "Migration 2"},
		{ID: "m3", Description: "Migration 3"},
	}
	pending, err := engine.Pending(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(pending))
	}
	if _, err := engine.Apply([]domain.Migration{all[0]}); err != nil {
		t.Fatal(err)
	}
	pending, err = engine.Pending(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].ID != "m2" || pending[1].ID != "m3" {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestEngineApplyFailure(t *testing.T) {
	engine := newEngine(&fakeStore{})
	executed := map[string]bool{}
	migrations := []domain.Migration{
		{ID: "m1", Description: "Migration 1", Run: func(string) error { executed["m1"] = true; return nil }},
		{ID: "m2", Description: "Migration 2", Run: func(string) error {
			executed["m2"] = true
			return fmt.Errorf("intentional failure in m2")
		}},
		{ID: "m3", Description: "Migration 3", Run: func(string) error { executed["m3"] = true; return nil }},
	}

	results, err := engine.Apply(migrations)
	if err == nil || !strings.Contains(err.Error(), "intentional failure in m2") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Success || results[0].ID != "m1" {
		t.Errorf("result 0 = %+v", results[0])
	}
	if results[1].Success || results[1].ID != "m2" || results[1].Error != "intentional failure in m2" {
		t.Errorf("result 1 = %+v", results[1])
	}
	if executed["m3"] {
		t.Error("m3 ran after m2 failed")
	}

	state, err := engine.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.IsApplied("m1") {
		t.Error("m1 not recorded")
	}
	if state.IsApplied("m2") || state.IsApplied("m3") {
		t.Error("m2 or m3 recorded")
	}
}

func TestApplyStateSaveFailure(t *testing.T) {
	engine := newEngine(&fakeStore{save: errors.New("disk full")})
	_, err := engine.Apply([]domain.Migration{{ID: "m1", Run: func(string) error { return nil }}})
	if err == nil || !strings.Contains(err.Error(), "failed to persist state") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadStateError(t *testing.T) {
	engine := newEngine(&fakeStore{load: errors.New("corrupt")})
	if _, err := engine.LoadState(); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := engine.Pending(nil); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := engine.Apply(nil); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRunPendingUsesBuiltins(t *testing.T) {
	res, err := newEngine(&fakeStore{}).RunPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 0 || res.Applied != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.WorkspaceRoot != "/ws" {
		t.Errorf("workspace root = %q", res.WorkspaceRoot)
	}
}

func TestStatusCounts(t *testing.T) {
	now := time.Date(2026, 8, 31, 14, 30, 0, 0, time.UTC)
	// Status only lists built-in migrations, which are empty, but it still
	// reports the state file and normalised counts.
	res, err := newEngine(&fakeStore{state: &domain.MigrationState{
		Version: domain.DefaultStateVersion,
		Applied: []domain.AppliedMigrationRecord{{ID: "001_workspace_init", AppliedAt: now}},
	}}).Status()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatePath != "/ws/.orbit/migrations.json" {
		t.Errorf("state path = %q", res.StatePath)
	}
	if res.Applied != 0 || res.Pending != 0 {
		t.Errorf("counts = %d/%d", res.Applied, res.Pending)
	}
	if res.Entries == nil {
		t.Error("entries is nil")
	}
}
