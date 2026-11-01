package domain

import "testing"

func TestIsApplied(t *testing.T) {
	var nilState *MigrationState
	if nilState.IsApplied("x") {
		t.Error("nil state reports applied")
	}

	state := &MigrationState{Applied: []AppliedMigrationRecord{{ID: "m1"}}}
	if !state.IsApplied("m1") {
		t.Error("m1 not reported applied")
	}
	if state.IsApplied("m2") {
		t.Error("m2 reported applied")
	}
}

func TestGetBuiltinMigrations(t *testing.T) {
	if got := len(GetBuiltinMigrations()); got != 0 {
		t.Errorf("expected 0 default builtin migrations (bootstrapped in orbit init), got %d", got)
	}
}
