package runnertest

import (
	"context"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
)

func TestFakeRecordsCallsInOrder(t *testing.T) {
	f := &Fake{}
	f.On("git rev-parse", runner.Result{Stdout: "abc"}, nil)
	f.On("gh pr", runner.Result{Stdout: "[]"}, nil)

	ctx := context.Background()
	if _, err := f.Run(ctx, "/repo", "git", "rev-parse", "HEAD"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := f.Run(ctx, "", "gh", "pr", "list"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	calls := f.Calls()
	if len(calls) != 2 {
		t.Fatalf("Calls len = %d, want 2", len(calls))
	}
	if calls[0].Dir != "/repo" || calls[0].Name != "git" {
		t.Errorf("first call = %+v", calls[0])
	}
	if got := calls[0].Args; len(got) != 2 || got[0] != "rev-parse" || got[1] != "HEAD" {
		t.Errorf("first call args = %v", got)
	}
	if calls[1].Name != "gh" {
		t.Errorf("second call = %+v", calls[1])
	}
}

func TestFakePrefixMatching(t *testing.T) {
	f := &Fake{}
	f.On("git status", runner.Result{Stdout: "status"}, nil)
	f.On("git", runner.Result{Stdout: "generic"}, nil)

	res, err := f.Run(context.Background(), "", "git", "status", "--short")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "status" {
		t.Errorf("Stdout = %q, want %q (first match wins)", res.Stdout, "status")
	}

	res, err = f.Run(context.Background(), "", "git", "diff")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "generic" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "generic")
	}
}

func TestFakeUnexpectedCommand(t *testing.T) {
	f := &Fake{}
	_, err := f.Run(context.Background(), "", "git", "push")
	if err == nil {
		t.Fatal("Run returned nil error for an unmatched command")
	}
	want := "runnertest: unexpected command: git push"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if got := f.Calls(); len(got) != 1 {
		t.Errorf("Calls len = %d, want 1 (unmatched calls are still recorded)", len(got))
	}
}

func TestFakeScriptedError(t *testing.T) {
	f := &Fake{}
	f.On("gh", runner.Result{Stderr: "nope", ExitCode: 1}, context.DeadlineExceeded)
	res, err := f.Run(context.Background(), "", "gh", "auth", "status")
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v, want the scripted error", err)
	}
	if res.ExitCode != 1 || res.Stderr != "nope" {
		t.Errorf("result = %+v", res)
	}
}
