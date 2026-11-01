package pr

import (
	"strings"
	"testing"
)

func TestEvaluateChecks(t *testing.T) {
	tests := []struct {
		name        string
		checks      []Check
		wantAllowed bool
		wantReason  string
	}{
		{
			name:        "AllPass",
			checks:      []Check{{Name: "ci", Bucket: "pass"}, {Name: "lint", Bucket: "pass"}},
			wantAllowed: true,
		},
		{
			name:        "PassAndSkipping",
			checks:      []Check{{Name: "ci", Bucket: "pass"}, {Name: "deploy", Bucket: "skipping"}},
			wantAllowed: true,
		},
		{
			name:       "SingleFail",
			checks:     []Check{{Name: "ci", Bucket: "pass"}, {Name: "lint", Bucket: "fail"}},
			wantReason: "checks failed: lint",
		},
		{
			name:       "Cancel",
			checks:     []Check{{Name: "deploy", Bucket: "cancel"}},
			wantReason: "checks failed: deploy",
		},
		{
			name:       "TwoFailsListBoth",
			checks:     []Check{{Name: "ci", Bucket: "fail"}, {Name: "lint", Bucket: "fail"}},
			wantReason: "checks failed: ci, lint",
		},
		{
			name:       "Pending",
			checks:     []Check{{Name: "ci", Bucket: "pending"}},
			wantReason: "checks still running: ci",
		},
		{
			name:       "FailBeatsPending",
			checks:     []Check{{Name: "ci", Bucket: "pending"}, {Name: "lint", Bucket: "fail"}},
			wantReason: "checks failed: lint",
		},
		{
			name:       "EmptyList",
			checks:     nil,
			wantReason: "no checks reported",
		},
		{
			name:       "UnknownBucket",
			checks:     []Check{{Name: "ci", Bucket: "weird"}},
			wantReason: `unknown check state "weird" for ci`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateChecks(tt.checks)
			if got.Allowed != tt.wantAllowed {
				t.Fatalf("Allowed = %v, want %v (reason %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestParseChecks(t *testing.T) {
	t.Run("ValidArray", func(t *testing.T) {
		checks, err := ParseChecks([]byte(`[{"name":"ci","bucket":"pass"},{"name":"lint","bucket":"pending"}]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(checks) != 2 {
			t.Fatalf("expected 2 checks, got %d", len(checks))
		}
		if checks[0].Name != "ci" || checks[0].Bucket != "pass" {
			t.Errorf("unexpected first check: %+v", checks[0])
		}
		if checks[1].Name != "lint" || checks[1].Bucket != "pending" {
			t.Errorf("unexpected second check: %+v", checks[1])
		}
	})

	t.Run("EmptyArray", func(t *testing.T) {
		checks, err := ParseChecks([]byte(`[]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(checks) != 0 {
			t.Fatalf("expected no checks, got %d", len(checks))
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		_, err := ParseChecks([]byte(`not json`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "checks") {
			t.Errorf("expected error to mention checks, got: %v", err)
		}
	})
}
