package domain

import (
	"strings"
	"testing"

	prdomain "github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

func TestIsReleaseHead(t *testing.T) {
	for _, head := range []string{"changeset-release/main", "release-please--branches--main"} {
		if !IsReleaseHead(head) {
			t.Errorf("%q should be a release head", head)
		}
	}
	for _, head := range []string{"main", "feat/x", "changeset-release/other"} {
		if IsReleaseHead(head) {
			t.Errorf("%q should not be a release head", head)
		}
	}
}

func TestCheckState(t *testing.T) {
	tests := []struct {
		name   string
		checks []prdomain.Check
		want   string
	}{
		{"None", nil, "none"},
		{"Pass", []prdomain.Check{{Name: "ci", Bucket: "pass"}}, "pass"},
		{"Skipped", []prdomain.Check{{Name: "ci", Bucket: "skipping"}}, "pass"},
		{"Fail", []prdomain.Check{{Name: "ci", Bucket: "fail"}}, "fail"},
		{"Cancel", []prdomain.Check{{Name: "ci", Bucket: "cancel"}}, "fail"},
		{"Pending", []prdomain.Check{{Name: "ci", Bucket: "pending"}, {Name: "docs", Bucket: "pass"}}, "pending"},
		{"Unknown", []prdomain.Check{{Name: "ci", Bucket: "mystery"}}, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckState(tt.checks); got != tt.want {
				t.Errorf("CheckState = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsPublishOrDeployWorkflow(t *testing.T) {
	for _, name := range []string{"Publish", "publish.yml", "Deploy", "deploy-ssh"} {
		if !IsPublishOrDeployWorkflow(name) {
			t.Errorf("%q should count as publish or deploy", name)
		}
	}
	for _, name := range []string{"CI", "CodeQL", "Release"} {
		if IsPublishOrDeployWorkflow(name) {
			t.Errorf("%q should not count as publish or deploy", name)
		}
	}
}

func TestValidDatabaseName(t *testing.T) {
	if !ValidDatabaseName("test_abc123") {
		t.Error("test_abc123 should be valid")
	}
	for _, name := range []string{"", "test_", "prod", "test_ABC", "test_abc;drop"} {
		if ValidDatabaseName(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestReplaceDatabaseName(t *testing.T) {
	got, err := ReplaceDatabaseName("postgres://user:pass@localhost:5432/dev?sslmode=disable", "test_abc123")
	if err != nil {
		t.Fatalf("ReplaceDatabaseName: %v", err)
	}
	if !strings.Contains(got, "/test_abc123?") || strings.Contains(got, "/dev?") {
		t.Errorf("url = %q", got)
	}
	if _, err := ReplaceDatabaseName("not a url", "test_abc123"); err == nil {
		t.Error("expected an invalid URL error")
	}
}

func TestRedact(t *testing.T) {
	got := Redact("connect to postgres://u:pass@h/db failed", []string{"postgres://u:pass@h/db"})
	if strings.Contains(got, "pass") || !strings.Contains(got, "<redacted>") {
		t.Errorf("redacted = %q", got)
	}
}
