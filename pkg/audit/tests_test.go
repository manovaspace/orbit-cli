package audit_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

func TestTestsListsRepositoriesWithoutTests(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{
		{Name: "clean", Path: "orbit/clean", Dir: filepath.Join(root, "orbit", "clean"), DefaultBranch: "main"},
		{Name: "no-tests", Path: "clients/demo/no-tests", Dir: filepath.Join(root, "clients", "demo", "no-tests"), DefaultBranch: "main"},
		{Name: "docs", Path: "manovaspace/docs", Dir: filepath.Join(root, "manovaspace", "docs"), DefaultBranch: "main"},
	}

	result := service.Tests(context.Background(), false)
	if result.ExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0: %+v", result.ExitCode(), result.Report)
	}
	f := findFinding(result.Report, "tests.missing", "clients/demo/no-tests")
	if f == nil {
		t.Fatalf("no tests.missing finding: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityWarn {
		t.Errorf("severity = %s, want WARN", f.Severity)
	}
	if findFinding(result.Report, "tests.missing", "orbit/clean") != nil {
		t.Error("repository with tests reported")
	}
	if findFinding(result.Report, "tests.missing", "manovaspace/docs") != nil {
		t.Error("static-only site reported")
	}
}

func TestTestsActionsMinutesUnverifiedWhenRefused(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{{
		Name:          "clean",
		Path:          "orbit/clean",
		Dir:           filepath.Join(root, "orbit", "clean"),
		DefaultBranch: "main",
		RemoteURL:     "git@github.com:manovaspace/orbit-cli.git",
	}}
	service.GitHub = &fakeGitHub{usageErr: errors.New("gh api: HTTP 403")}

	result := service.Tests(context.Background(), true)
	if result.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2: %+v", result.ExitCode(), result.Report)
	}
	joined := strings.Join(result.Unverified, "\n")
	if !strings.Contains(joined, "actions minutes") || !strings.Contains(joined, "403") {
		t.Errorf("unverified = %q, want the refused billing API", joined)
	}
}

func TestTestsActionsMinutesInfo(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{{
		Name:          "clean",
		Path:          "orbit/clean",
		Dir:           filepath.Join(root, "orbit", "clean"),
		DefaultBranch: "main",
		RemoteURL:     "git@github.com:manovaspace/orbit-cli.git",
	}}
	service.GitHub = &fakeGitHub{usage: domain.ActionsUsage{TotalMinutesUsed: 1200, IncludedMinutes: 2000}}

	result := service.Tests(context.Background(), true)
	f := findFinding(result.Report, "tests.actions-minutes", "manovaspace")
	if f == nil {
		t.Fatalf("no actions-minute finding: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityInfo {
		t.Errorf("severity = %s, want INFO", f.Severity)
	}
}
