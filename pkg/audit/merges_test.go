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
			{Number: 1, Title: "red", URL: "https://github.com/manovaspace/orbit-cli/pull/1", HeadRef: "feat/red", Checks: []domain.MergedCheck{{Name: "ci", Bucket: "fail"}}},
			{Number: 2, Title: "release empty", URL: "https://github.com/manovaspace/orbit-cli/pull/2", HeadRef: "changeset-release/main"},
			{Number: 3, Title: "override", URL: "https://github.com/manovaspace/orbit-cli/pull/3", HeadRef: "renovate/x", Comments: []string{"lgtm", "override: production hotfix"}},
			{Number: 4, Title: "green", URL: "https://github.com/manovaspace/orbit-cli/pull/4", HeadRef: "renovate/y", Checks: []domain.MergedCheck{{Name: "ci", Bucket: "pass"}}},
			{Number: 5, Title: "feature empty", URL: "https://github.com/manovaspace/orbit-cli/pull/5", HeadRef: "feat/quiet"},
			{Number: 6, Title: "renovate skipped", URL: "https://github.com/manovaspace/orbit-cli/pull/6", HeadRef: "renovate/z", Checks: []domain.MergedCheck{{Name: "ci", Bucket: "skipping"}}},
			{Number: 7, Title: "renovate secret only", URL: "https://github.com/manovaspace/orbit-cli/pull/7", HeadRef: "renovate/w", Checks: []domain.MergedCheck{{Name: "secret-scan / gitleaks", Bucket: "pass"}}},
			{Number: 8, Title: "feature secret fail", URL: "https://github.com/manovaspace/orbit-cli/pull/8", HeadRef: "feat/leak", Checks: []domain.MergedCheck{{Name: "secret-scan / gitleaks", Bucket: "fail"}}},
			{Number: 9, Title: "feature skipped", URL: "https://github.com/manovaspace/orbit-cli/pull/9", HeadRef: "feat/skipped", Checks: []domain.MergedCheck{{Name: "docs", Bucket: "skipping"}}},
		},
	}}

	result := service.Merges(context.Background(), 30, true)
	if result.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1: %+v", result.ExitCode(), result.Report)
	}
	want := map[string]string{
		"#1": "checks failed: ci",
		"#2": "no CI check passed",
		"#6": "no CI check passed",
		"#7": "no CI check passed",
		"#8": "checks failed: secret-scan / gitleaks",
	}
	if got := countFindings(result.Report, "merges.bypass"); got != len(want) {
		t.Fatalf("merges.bypass findings = %d, want %d: %+v", got, len(want), result.Findings)
	}
	for subject, reason := range want {
		f := findFinding(result.Report, "merges.bypass", subject)
		if f == nil || f.Actual != reason {
			t.Errorf("finding %s = %+v, want reason %q", subject, f, reason)
		}
	}
	for _, subject := range []string{"#3", "#4", "#5", "#9"} {
		if findFinding(result.Report, "merges.bypass", subject) != nil {
			t.Errorf("%s flagged, want clean", subject)
		}
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
