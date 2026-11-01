package ghcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

var testRepo = domain.Repo{Owner: "manovaspace", Name: "orbit-test"}

func TestCreatePRArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr create", runner.Result{Stdout: "https://github.com/manovaspace/orbit-test/pull/42\n"}, nil)

	created, err := New(fake).CreatePR(context.Background(), testRepo, "feat: thing", "body text", "feat/thing", "main")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}

	want := []string{"pr", "create", "-R", "manovaspace/orbit-test", "--title", "feat: thing", "--base", "main", "--head", "feat/thing", "--body", "body text"}
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(calls), calls)
	}
	if calls[0].Name != "gh" {
		t.Errorf("command = %q, want gh", calls[0].Name)
	}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("args = %v, want %v", calls[0].Args, want)
	}
	if created.Number != 42 {
		t.Errorf("number = %d, want 42", created.Number)
	}
	if created.URL != "https://github.com/manovaspace/orbit-test/pull/42" {
		t.Errorf("url = %q", created.URL)
	}
}

func TestCreatePREmptyBodyStillPassesBodyFlag(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr create", runner.Result{Stdout: "https://github.com/manovaspace/orbit-test/pull/7"}, nil)

	if _, err := New(fake).CreatePR(context.Background(), testRepo, "title", "", "head", "main"); err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	args := fake.Calls()[0].Args
	if args[len(args)-2] != "--body" || args[len(args)-1] != "" {
		t.Errorf("expected a trailing empty --body, got %v", args)
	}
}

func TestMergePRArgs(t *testing.T) {
	tests := []struct {
		method domain.Method
		want   string
	}{
		{method: domain.MethodMerge, want: "--merge"},
		{method: domain.MethodSquash, want: "--squash"},
		{method: domain.MethodRebase, want: "--rebase"},
	}
	for _, tt := range tests {
		t.Run(string(tt.method), func(t *testing.T) {
			fake := &runnertest.Fake{}
			fake.On("gh pr merge", runner.Result{}, nil)

			if err := New(fake).MergePR(context.Background(), testRepo, "7", tt.method); err != nil {
				t.Fatalf("MergePR: %v", err)
			}
			want := []string{"pr", "merge", "7", "-R", "manovaspace/orbit-test", tt.want}
			if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
				t.Errorf("args = %v, want %v", got, want)
			}
		})
	}
}

func TestChecksArgsAndParsesNonzeroExit(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr checks", runner.Result{Stdout: `[{"name":"ci","bucket":"fail"}]`}, errors.New("exit status 1"))

	checks, err := New(fake).Checks(context.Background(), testRepo, "7")
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	want := []string{"pr", "checks", "7", "-R", "manovaspace/orbit-test", "--json", "name,bucket"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if len(checks) != 1 || checks[0].Name != "ci" || checks[0].Bucket != "fail" {
		t.Errorf("checks = %+v, want one failing ci check", checks)
	}
}

func TestChecksNoChecksReportedMapsToEmpty(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr checks", runner.Result{Stderr: "no checks reported on the 'feat/x' branch\n"}, errors.New("exit status 1"))

	checks, err := New(fake).Checks(context.Background(), testRepo, "7")
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	if len(checks) != 0 {
		t.Errorf("checks = %+v, want none", checks)
	}
}

func TestChecksOtherFailureIsReturned(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr checks", runner.Result{Stdout: "garbage\n", Stderr: "boom: unexpected failure\n"}, errors.New("exit status 1"))

	_, err := New(fake).Checks(context.Background(), testRepo, "7")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected gh stderr in the error, got: %v", err)
	}
	if got := clierr.Code(err); got != clierr.CodeTool {
		t.Errorf("code = %q, want %q", got, clierr.CodeTool)
	}
}

func TestChecksTransportFailureIsNetwork(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr checks", runner.Result{Stderr: "dial tcp: lookup github.com: no such host\n"}, errors.New("exit status 1"))

	_, err := New(fake).Checks(context.Background(), testRepo, "7")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got := clierr.Code(err); got != clierr.CodeNetwork {
		t.Errorf("code = %q, want %q", got, clierr.CodeNetwork)
	}
}

func TestCommentArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr comment", runner.Result{}, nil)

	if err := New(fake).Comment(context.Background(), testRepo, "7", "override: hotfix"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	want := []string{"pr", "comment", "7", "-R", "manovaspace/orbit-test", "--body", "override: hotfix"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestCommentFailureKeepsStderr(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr comment", runner.Result{Stderr: "comment rejected\n"}, errors.New("exit status 1"))

	err := New(fake).Comment(context.Background(), testRepo, "7", "body")
	if err == nil || !strings.Contains(err.Error(), "comment rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListOpenArgsAndParses(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr list", runner.Result{Stdout: `[{"number":12,"title":"feat: x","headRefName":"feat/x","baseRefName":"main","author":{"login":"opmc"},"url":"https://github.com/manovaspace/orbit-test/pull/12"}]`}, nil)

	open, err := New(fake).ListOpen(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	want := []string{"pr", "list", "-R", "manovaspace/orbit-test", "--state", "open", "--json", "number,title,headRefName,baseRefName,author,url"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if len(open) != 1 {
		t.Fatalf("got %d prs, want 1", len(open))
	}
	if open[0].Number != 12 || open[0].Head != "feat/x" || open[0].Base != "main" {
		t.Errorf("unexpected pull request: %+v", open[0])
	}
	if open[0].Author != "opmc" {
		t.Errorf("author = %q, want opmc", open[0].Author)
	}
}

func TestListOpenWithoutAuthorLeavesItEmpty(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr list", runner.Result{Stdout: `[{"number":12,"title":"feat: x","headRefName":"feat/x","baseRefName":"main","author":null,"url":"https://github.com/manovaspace/orbit-test/pull/12"}]`}, nil)

	open, err := New(fake).ListOpen(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("got %d prs, want 1", len(open))
	}
	if open[0].Author != "" {
		t.Errorf("author = %q, want empty", open[0].Author)
	}
}

func TestListOpenRejectsBadJSON(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh pr list", runner.Result{Stdout: "not json"}, nil)

	if _, err := New(fake).ListOpen(context.Background(), testRepo); err == nil {
		t.Fatal("expected an error, got nil")
	}
}
