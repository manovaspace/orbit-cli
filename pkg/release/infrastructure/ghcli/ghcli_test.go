package ghcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

const testSlug = "manovaspace/orbit-cli"

func TestCommitChecksArgsAndBuckets(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh api repos/"+testSlug+"/commits/abc123/check-runs", runner.Result{Stdout: `{"name":"ci","status":"completed","conclusion":"success"}
{"name":"lint","status":"completed","conclusion":"failure"}
{"name":"docs","status":"in_progress","conclusion":""}
`}, nil)

	checks, err := New(fake).CommitChecks(context.Background(), testSlug, "abc123")
	if err != nil {
		t.Fatalf("CommitChecks: %v", err)
	}
	want := []string{"api", "repos/" + testSlug + "/commits/abc123/check-runs", "--paginate", "--jq", ".check_runs[]"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if len(checks) != 3 || checks[0].Bucket != "pass" || checks[1].Bucket != "fail" || checks[2].Bucket != "pending" {
		t.Errorf("checks = %+v", checks)
	}
}

func TestBranchSHAArgsAndMissing(t *testing.T) {
	t.Run("Exists", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("gh api repos/"+testSlug+"/git/ref/heads/demo", runner.Result{Stdout: "abc123\n"}, nil)

		sha, exists, err := New(fake).BranchSHA(context.Background(), testSlug, "demo")
		if err != nil || !exists || sha != "abc123" {
			t.Fatalf("sha = %q exists = %v err = %v", sha, exists, err)
		}
		want := []string{"api", "repos/" + testSlug + "/git/ref/heads/demo", "--jq", ".object.sha"}
		if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
			t.Errorf("args = %v, want %v", got, want)
		}
	})
	t.Run("Missing", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("gh api repos/"+testSlug+"/git/ref/heads/demo", runner.Result{Stderr: "HTTP 404: Not Found\n"}, errors.New("exit status 1"))

		_, exists, err := New(fake).BranchSHA(context.Background(), testSlug, "demo")
		if err != nil || exists {
			t.Fatalf("exists = %v err = %v", exists, err)
		}
	})
	t.Run("OtherFailure", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("gh api repos/"+testSlug+"/git/ref/heads/demo", runner.Result{Stderr: "boom\n"}, errors.New("exit status 1"))

		_, _, err := New(fake).BranchSHA(context.Background(), testSlug, "demo")
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCreateBranchArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh api -X POST repos/"+testSlug+"/git/refs", runner.Result{}, nil)

	if err := New(fake).CreateBranch(context.Background(), testSlug, "demo", "abc123"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	want := []string{"api", "-X", "POST", "repos/" + testSlug + "/git/refs", "-f", "ref=refs/heads/demo", "-f", "sha=abc123"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestDispatchArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh workflow run publish.yml", runner.Result{}, nil)

	if err := New(fake).Dispatch(context.Background(), testSlug, "publish.yml", "main"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	want := []string{"workflow", "run", "publish.yml", "-R", testSlug, "--ref", "main"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestCompareAheadArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh api repos/"+testSlug+"/compare/demo...main", runner.Result{Stdout: "3\n"}, nil)

	ahead, err := New(fake).CompareAhead(context.Background(), testSlug, "demo", "main")
	if err != nil || ahead != 3 {
		t.Fatalf("ahead = %d err = %v", ahead, err)
	}
	want := []string{"api", "repos/" + testSlug + "/compare/demo...main", "--jq", ".ahead_by"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestRunsAndListOpenArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh run list", runner.Result{Stdout: `[{"name":"Publish","displayTitle":"chore: version packages","status":"completed","conclusion":"success","headBranch":"main","createdAt":"2026-01-01T00:00:00Z","url":"https://github.com/run/1"}]`}, nil)

	runs, err := New(fake).Runs(context.Background(), testSlug)
	if err != nil || len(runs) != 1 || runs[0].Name != "Publish" {
		t.Fatalf("runs = %+v err = %v", runs, err)
	}
	want := []string{"run", "list", "-R", testSlug, "--limit", "20", "--json", "name,displayTitle,status,conclusion,headBranch,createdAt,url"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestChecksNoChecksMapsToEmpty(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr checks", runner.Result{Stderr: "no checks reported on the 'changeset-release/main' branch\n"}, errors.New("exit status 1"))

	checks, err := New(fake).Checks(context.Background(), testSlug, "12")
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	if len(checks) != 0 {
		t.Errorf("checks = %+v", checks)
	}
}

func TestCommitChecksFallsBackToStatusWhenConclusionEmpty(t *testing.T) {
	if got := checkBucket("in_progress", ""); got != "pending" {
		t.Errorf("bucket = %q", got)
	}
	if got := checkBucket("completed", "skipped"); got != "skipping" {
		t.Errorf("bucket = %q", got)
	}
}
