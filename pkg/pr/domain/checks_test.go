package domain

import (
	"strings"
	"testing"
)

const noCIReason = "no CI check passed (release and Renovate pull requests need CI)"

func TestEvaluateChecks(t *testing.T) {
	ci := Check{Name: "ci", Bucket: "pass"}
	secret := Check{Name: "secret-scan / gitleaks", Bucket: "pass"}
	secretSkipped := Check{Name: "secret-scan / gitleaks", Bucket: "skipping"}
	skipped := Check{Name: "deploy", Bucket: "skipping"}

	tests := []struct {
		name       string
		checks     []Check
		requireCI  bool
		wantAllow  bool
		wantReason string
	}{
		// Non-release pull requests: empty, skipped and secret-scan-only lists merge.
		{name: "Empty/feature", checks: nil, requireCI: false, wantAllow: true},
		{name: "AllSkipped/feature", checks: []Check{skipped, secretSkipped}, requireCI: false, wantAllow: true},
		{name: "SecretOnly/feature", checks: []Check{secret}, requireCI: false, wantAllow: true},
		{name: "SecretSkippedOnly/feature", checks: []Check{secretSkipped}, requireCI: false, wantAllow: true},
		{name: "Pass/feature", checks: []Check{ci}, requireCI: false, wantAllow: true},
		{name: "PassAndSkipping/feature", checks: []Check{ci, skipped}, requireCI: false, wantAllow: true},
		{name: "Fail/feature", checks: []Check{ci, {Name: "lint", Bucket: "fail"}}, requireCI: false, wantReason: "checks failed: lint"},
		{name: "Cancel/feature", checks: []Check{{Name: "deploy", Bucket: "cancel"}}, requireCI: false, wantReason: "checks failed: deploy"},
		{name: "TwoFailsListBoth/feature", checks: []Check{{Name: "ci", Bucket: "fail"}, {Name: "lint", Bucket: "fail"}}, requireCI: false, wantReason: "checks failed: ci, lint"},
		{name: "SecretFail/feature", checks: []Check{{Name: "secret-scan / gitleaks", Bucket: "fail"}}, requireCI: false, wantReason: "checks failed: secret-scan / gitleaks"},
		{name: "Pending/feature", checks: []Check{{Name: "ci", Bucket: "pending"}}, requireCI: false, wantReason: "checks still running: ci"},
		{name: "SecretPending/feature", checks: []Check{{Name: "secret-scan / gitleaks", Bucket: "pending"}}, requireCI: false, wantReason: "checks still running: secret-scan / gitleaks"},
		{name: "FailBeatsPending/feature", checks: []Check{{Name: "ci", Bucket: "pending"}, {Name: "lint", Bucket: "fail"}}, requireCI: false, wantReason: "checks failed: lint"},
		{name: "Unknown/feature", checks: []Check{{Name: "ci", Bucket: "weird"}}, requireCI: false, wantReason: `unknown check state "weird" for ci`},

		// Release and Renovate pull requests: a passing non-secret-scan check is required.
		{name: "Empty/release", checks: nil, requireCI: true, wantReason: "no checks reported"},
		{name: "AllSkipped/release", checks: []Check{skipped, secretSkipped}, requireCI: true, wantReason: noCIReason},
		{name: "SecretOnly/release", checks: []Check{secret}, requireCI: true, wantReason: noCIReason},
		{name: "SecretSkippedOnly/release", checks: []Check{secretSkipped}, requireCI: true, wantReason: noCIReason},
		{name: "Pass/release", checks: []Check{ci}, requireCI: true, wantAllow: true},
		{name: "PassAndSecret/release", checks: []Check{ci, secret}, requireCI: true, wantAllow: true},
		{name: "PassAndSkipping/release", checks: []Check{ci, skipped}, requireCI: true, wantAllow: true},
		{name: "SecretPassCISkipped/release", checks: []Check{secret, skipped}, requireCI: true, wantReason: noCIReason},
		{name: "Fail/release", checks: []Check{ci, {Name: "lint", Bucket: "fail"}}, requireCI: true, wantReason: "checks failed: lint"},
		{name: "SecretFailCIPass/release", checks: []Check{ci, {Name: "secret-scan / gitleaks", Bucket: "fail"}}, requireCI: true, wantReason: "checks failed: secret-scan / gitleaks"},
		{name: "Pending/release", checks: []Check{{Name: "ci", Bucket: "pending"}}, requireCI: true, wantReason: "checks still running: ci"},
		{name: "Unknown/release", checks: []Check{{Name: "ci", Bucket: "weird"}}, requireCI: true, wantReason: `unknown check state "weird" for ci`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateChecks(tt.checks, tt.requireCI)
			if got.Allowed != tt.wantAllow {
				t.Fatalf("Allowed = %v, want %v (reason %q)", got.Allowed, tt.wantAllow, got.Reason)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestRequiresCI(t *testing.T) {
	tests := map[string]bool{
		"release-please--branches--main":  true,
		"changeset-release/main":          true,
		"renovate/golang-x-net-0.x":       true,
		"feat/dev-stage-ci":               false,
		"fix/renovate-config":             false,
		"release/1.0":                     false,
		"":                                false,
		"xrelease-please--branches--main": false,
	}
	for head, want := range tests {
		if got := RequiresCI(head); got != want {
			t.Errorf("RequiresCI(%q) = %v, want %v", head, got, want)
		}
	}
}

func TestIsSecretScan(t *testing.T) {
	if !IsSecretScan("secret-scan / gitleaks") {
		t.Error("secret-scan check not recognised")
	}
	if !IsSecretScan("secret-scan") {
		t.Error("bare secret-scan check not recognised")
	}
	for _, name := range []string{"ci / test", "lint", "Secret-scan / gitleaks", ""} {
		if IsSecretScan(name) {
			t.Errorf("IsSecretScan(%q) = true, want false", name)
		}
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
