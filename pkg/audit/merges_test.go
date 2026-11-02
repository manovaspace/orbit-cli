package audit_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	"github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

func mergeWorkspace(t *testing.T) *application.Service {
	t.Helper()
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{{
		Name:          "orbit-cli",
		Path:          "orbit/orbit-cli",
		Dir:           filepath.Join(root, "orbit", "clean"),
		DefaultBranch: "main",
		RemoteURL:     "git@github.com:manovaspace/orbit-cli.git",
	}}
	service.Now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
	return service
}

func TestMergesFlagsBypassAndIgnoresOverride(t *testing.T) {
	service := mergeWorkspace(t)
	service.GitHub = &fakeGitHub{prs: map[string][]domain.MergedPullRequest{
		"manovaspace/orbit-cli": {
			{Number: 1, Title: "red", URL: "https://github.com/manovaspace/orbit-cli/pull/1", Checks: []domain.MergedCheck{{Name: "ci", Bucket: "fail"}}},
			{Number: 2, Title: "empty", URL: "https://github.com/manovaspace/orbit-cli/pull/2"},
			{Number: 3, Title: "override", URL: "https://github.com/manovaspace/orbit-cli/pull/3", Comments: []string{"lgtm", "override: production hotfix"}},
			{Number: 4, Title: "green", URL: "https://github.com/manovaspace/orbit-cli/pull/4", Checks: []domain.MergedCheck{{Name: "ci", Bucket: "pass"}}},
		},
	}}

	result := service.Merges(context.Background(), 30, true)
	if result.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1: %+v", result.ExitCode(), result.Report)
	}
	if got := countFindings(result.Report, "merges.bypass"); got != 2 {
		t.Fatalf("merges.bypass findings = %d, want 2: %+v", got, result.Findings)
	}
	if f := findFinding(result.Report, "merges.bypass", "#1"); f == nil || !strings.Contains(f.Actual, "ci") {
		t.Errorf("failed check not reported: %+v", f)
	}
	if f := findFinding(result.Report, "merges.bypass", "#2"); f == nil || !strings.Contains(f.Actual, "no checks") {
		t.Errorf("no checks not reported: %+v", f)
	}
	if findFinding(result.Report, "merges.bypass", "#3") != nil {
		t.Error("override PR flagged")
	}
	if findFinding(result.Report, "merges.bypass", "#4") != nil {
		t.Error("passing PR flagged")
	}
}

func TestMergesGHFailureIsUnverified(t *testing.T) {
	service := mergeWorkspace(t)
	service.GitHub = &fakeGitHub{prErr: map[string]error{
		"manovaspace/orbit-cli": errors.New("gh: HTTP 403"),
	}}

	result := service.Merges(context.Background(), 30, true)
	if result.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2: %+v", result.ExitCode(), result.Report)
	}
	if !strings.Contains(strings.Join(result.Unverified, "\n"), "HTTP 403") {
		t.Errorf("unverified = %q, want the gh error", result.Unverified)
	}
}

func TestMergesPassesSinceToForge(t *testing.T) {
	service := mergeWorkspace(t)
	forge := &fakeGitHub{}
	service.GitHub = forge

	_ = service.Merges(context.Background(), 7, true)
	want := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if !forge.since.Equal(want) {
		t.Errorf("since = %s, want %s", forge.since, want)
	}
}

func TestMergesWithoutRemoteMakesNoRequest(t *testing.T) {
	service := mergeWorkspace(t)
	forge := &fakeGitHub{}
	service.GitHub = forge

	result := service.Merges(context.Background(), 30, false)
	if result.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", result.ExitCode())
	}
	if !strings.Contains(strings.Join(result.Unverified, "\n"), "remote checks need --remote") {
		t.Errorf("unverified = %q", result.Unverified)
	}
}
