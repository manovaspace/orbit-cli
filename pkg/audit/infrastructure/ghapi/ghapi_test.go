package ghapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestMergedPullRequestsReadsChecksAndComments(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{Stdout: `[
  {"number":1,"title":"red","url":"https://example/1","mergedAt":"2026-11-01T00:00:00Z","comments":[{"body":"lgtm"}],"statusCheckRollup":[{"__typename":"CheckRun","name":"ci","status":"COMPLETED","conclusion":"FAILURE"}]},
  {"number":2,"title":"override","url":"https://example/2","mergedAt":"2026-11-01T00:00:00Z","comments":[{"body":"override: hotfix"}],"statusCheckRollup":[]},
  {"number":3,"title":"context","url":"https://example/3","mergedAt":"2026-11-01T00:00:00Z","comments":[],"statusCheckRollup":[{"__typename":"StatusContext","context":"build","state":"SUCCESS"}]}
]`}, nil)

	prs, err := New(fake).MergedPullRequests(context.Background(), "manovaspace/orbit-cli", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MergedPullRequests: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("prs = %d, want 3", len(prs))
	}
	if len(prs[0].Checks) != 1 || prs[0].Checks[0].Bucket != "fail" {
		t.Errorf("check run not mapped: %+v", prs[0].Checks)
	}
	if !prs[1].HasOverride() {
		t.Error("override comment not parsed")
	}
	if len(prs[2].Checks) != 1 || prs[2].Checks[0].Name != "build" || prs[2].Checks[0].Bucket != "pass" {
		t.Errorf("status context not mapped: %+v", prs[2].Checks)
	}
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	line := strings.Join(append([]string{calls[0].Name}, calls[0].Args...), " ")
	if !strings.Contains(line, "--search merged:>=2026-10-01") {
		t.Errorf("command = %q, want the merged search", line)
	}
}

func TestMergedPullRequestsReportsGHError(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr list", runner.Result{Stderr: "HTTP 403: forbidden\n", ExitCode: 1}, errors.New("exit status 1"))

	if _, err := New(fake).MergedPullRequests(context.Background(), "manovaspace/orbit-cli", time.Now()); err == nil {
		t.Fatal("gh failure accepted")
	} else if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want gh stderr", err)
	}
}

func TestActionsUsageReadsBilling(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh api organizations/manovaspace/settings/billing/usage", runner.Result{Stdout: `{"usageItems":[
  {"product":"actions","unitType":"Minutes","quantity":1200},
  {"product":"actions","unitType":"Minutes","quantity":300},
  {"product":"actions","unitType":"GigabyteHours","quantity":9},
  {"product":"copilot","unitType":"Minutes","quantity":99}
]}`}, nil)

	usage, err := New(fake).ActionsUsage(context.Background(), "manovaspace")
	if err != nil {
		t.Fatalf("ActionsUsage: %v", err)
	}
	if usage.TotalMinutesUsed != 1500 {
		t.Errorf("usage = %+v, want 1500 minutes", usage)
	}
}

func TestActionsUsageReportsRefusal(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh api organizations/manovaspace/settings/billing/usage", runner.Result{Stderr: "HTTP 403: forbidden\n", ExitCode: 1}, errors.New("exit status 1"))
	fake.On("gh api users/manovaspace/settings/billing/usage", runner.Result{Stderr: "HTTP 404: not found\n", ExitCode: 1}, errors.New("exit status 1"))

	if _, err := New(fake).ActionsUsage(context.Background(), "manovaspace"); err == nil {
		t.Fatal("refused billing API accepted")
	} else if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want the first refusal", err)
	}
}

func TestReleasesFallsBackToNewestTag(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh api repos/golang/go/releases/latest", runner.Result{Stderr: "gh: Not Found (HTTP 404)\n", ExitCode: 1}, errors.New("exit status 1"))
	fake.On("gh api repos/golang/go/tags", runner.Result{Stdout: "go1.26.7\ngo1.27.3\nrelease-branch.go1.28\n"}, nil)

	tag, err := New(fake).Releases(context.Background(), "golang", "go")
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if tag != "go1.27.3" {
		t.Errorf("tag = %q, want the newest numeric tag", tag)
	}
}

func TestReleasesReadsLatestTag(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh api repos/golang/go/releases/latest", runner.Result{Stdout: "go1.27.3\n"}, nil)

	tag, err := New(fake).Releases(context.Background(), "golang", "go")
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if tag != "go1.27.3" {
		t.Errorf("tag = %q", tag)
	}
}
