package gitcli

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

const testDir = "/repo"

func TestRepoArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" config --get remote.origin.url", runner.Result{Stdout: "git@github.com:manovaspace/orbit-test.git\n"}, nil)
	fake.On("git -C "+testDir+" rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "feat/x\n"}, nil)

	info, err := New(fake).Repo(context.Background(), testDir)
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}

	want := [][]string{
		{"-C", testDir, "config", "--get", "remote.origin.url"},
		{"-C", testDir, "rev-parse", "--abbrev-ref", "HEAD"},
	}
	calls := fake.Calls()
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i].Name != "git" {
			t.Errorf("call %d command = %q, want git", i, calls[i].Name)
		}
		if calls[i].Dir != testDir {
			t.Errorf("call %d dir = %q, want %q", i, calls[i].Dir, testDir)
		}
		if !reflect.DeepEqual(calls[i].Args, want[i]) {
			t.Errorf("call %d args = %v, want %v", i, calls[i].Args, want[i])
		}
	}

	if info.String() != "manovaspace/orbit-test" {
		t.Errorf("repo = %s", info.Repo)
	}
	if info.CurrentBranch != "feat/x" {
		t.Errorf("branch = %q", info.CurrentBranch)
	}
	if !info.IsGitHub {
		t.Error("expected IsGitHub")
	}
}

func TestRepoFallsBackToRemoteGetURL(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" config --get remote.origin.url", runner.Result{}, errors.New("not set"))
	fake.On("git -C "+testDir+" remote get-url origin", runner.Result{Stdout: "https://git.internal.manova.space/manova/handbook.git\n"}, nil)
	fake.On("git -C "+testDir+" rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "main\n"}, nil)

	info, err := New(fake).Repo(context.Background(), testDir)
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if info.String() != "manova/handbook" {
		t.Errorf("repo = %s", info.Repo)
	}
	if info.IsGitHub {
		t.Error("expected IsGitHub to be false")
	}
	wantFallback := []string{"-C", testDir, "remote", "get-url", "origin"}
	if got := fake.Calls()[1].Args; !reflect.DeepEqual(got, wantFallback) {
		t.Errorf("fallback args = %v, want %v", got, wantFallback)
	}
}

func TestRepoResolvesDetachedHead(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" config --get remote.origin.url", runner.Result{Stdout: "git@github.com:o/r.git"}, nil)
	fake.On("git -C "+testDir+" rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "HEAD\n"}, nil)
	fake.On("git -C "+testDir+" symbolic-ref --short HEAD", runner.Result{Stdout: "feat/y\n"}, nil)

	info, err := New(fake).Repo(context.Background(), testDir)
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if info.CurrentBranch != "feat/y" {
		t.Errorf("branch = %q, want feat/y", info.CurrentBranch)
	}
	want := []string{"-C", testDir, "symbolic-ref", "--short", "HEAD"}
	if got := fake.Calls()[2].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestRepoMapsOriginLookupFailures(t *testing.T) {
	gitFailure := clierr.Tool("git -C " + testDir + " remote get-url origin exited with code 128")

	tests := []struct {
		name     string
		result   runner.Result
		cause    error
		wantCode string
		wantExit int
	}{
		{
			name:     "not a repository",
			result:   runner.Result{Stderr: "fatal: not a git repository (or any of the parent directories): .git\n"},
			cause:    gitFailure,
			wantCode: clierr.CodeUsage,
			wantExit: clierr.ExitUsage,
		},
		{
			name:     "missing git",
			result:   runner.Result{},
			cause:    clierr.Wrap(exec.ErrNotFound, clierr.Tool("cannot run git")),
			wantCode: clierr.CodeTool,
			wantExit: clierr.ExitTool,
		},
		{
			name:     "no origin remote",
			result:   runner.Result{Stderr: "error: No such remote 'origin'\n"},
			cause:    gitFailure,
			wantCode: clierr.CodeTool,
			wantExit: clierr.ExitTool,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &runnertest.Fake{}
			fake.On("git -C "+testDir+" config --get remote.origin.url", runner.Result{}, tc.cause)
			fake.On("git -C "+testDir+" remote get-url origin", tc.result, tc.cause)

			_, err := New(fake).Repo(context.Background(), testDir)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !errors.Is(err, tc.cause) {
				t.Errorf("errors.Is did not find the original cause: %v", err)
			}
			if got := clierr.Code(err); got != tc.wantCode {
				t.Errorf("Code = %q, want %q", got, tc.wantCode)
			}
			if got := clierr.ExitCode(err); got != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", got, tc.wantExit)
			}
			var c *clierr.Error
			if !errors.As(err, &c) {
				t.Fatalf("error is not a *clierr.Error: %v", err)
			}
			if c.Hint != repoOriginHint {
				t.Errorf("Hint = %q, want %q", c.Hint, repoOriginHint)
			}
			wantMessage := "not a git repository with an origin remote: " + testDir
			if got := err.Error(); got != wantMessage {
				t.Errorf("Error() = %q, want exactly %q", got, wantMessage)
			}
		})
	}
}

func TestIsClean(t *testing.T) {
	t.Run("Clean", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("git -C "+testDir+" status --porcelain", runner.Result{Stdout: ""}, nil)

		clean, err := New(fake).IsClean(context.Background(), testDir)
		if err != nil || !clean {
			t.Fatalf("clean = %v, err = %v", clean, err)
		}
		want := []string{"-C", testDir, "status", "--porcelain"}
		if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
			t.Errorf("args = %v, want %v", got, want)
		}
	})

	t.Run("Dirty", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("git -C "+testDir+" status --porcelain", runner.Result{Stdout: " M README.md\n"}, nil)

		clean, err := New(fake).IsClean(context.Background(), testDir)
		if err != nil || clean {
			t.Fatalf("clean = %v, err = %v", clean, err)
		}
	})
}

func TestLastCommitArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" log -1 --pretty=%s feat/x", runner.Result{Stdout: "feat: subject\n"}, nil)
	fake.On("git -C "+testDir+" log -1 --pretty=%b feat/x", runner.Result{Stdout: "body\n"}, nil)

	subject, body, err := New(fake).LastCommit(context.Background(), testDir, "feat/x")
	if err != nil {
		t.Fatalf("LastCommit: %v", err)
	}
	if subject != "feat: subject" || body != "body" {
		t.Errorf("got subject %q body %q", subject, body)
	}
	want := [][]string{
		{"-C", testDir, "log", "-1", "--pretty=%s", "feat/x"},
		{"-C", testDir, "log", "-1", "--pretty=%b", "feat/x"},
	}
	for i := range want {
		if got := fake.Calls()[i].Args; !reflect.DeepEqual(got, want[i]) {
			t.Errorf("call %d args = %v, want %v", i, got, want[i])
		}
	}
}

func TestPushCheckoutPullArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" push -u origin feat/x", runner.Result{}, nil)
	fake.On("git -C "+testDir+" checkout main", runner.Result{}, nil)
	fake.On("git -C "+testDir+" pull --ff-only", runner.Result{}, nil)

	git := New(fake)
	if err := git.Push(context.Background(), testDir, "feat/x"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := git.Checkout(context.Background(), testDir, "main"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if err := git.PullFFOnly(context.Background(), testDir); err != nil {
		t.Fatalf("PullFFOnly: %v", err)
	}

	want := [][]string{
		{"-C", testDir, "push", "-u", "origin", "feat/x"},
		{"-C", testDir, "checkout", "main"},
		{"-C", testDir, "pull", "--ff-only"},
	}
	for i := range want {
		if got := fake.Calls()[i].Args; !reflect.DeepEqual(got, want[i]) {
			t.Errorf("call %d args = %v, want %v", i, got, want[i])
		}
	}
}

func TestDeleteBranchFallsBackToForce(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" branch -d feat/x", runner.Result{Stderr: "error: the branch 'feat/x' is not fully merged\n"}, errors.New("exit status 1"))
	fake.On("git -C "+testDir+" branch -D feat/x", runner.Result{}, nil)

	if err := New(fake).DeleteBranch(context.Background(), testDir, "feat/x"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}

	want := [][]string{
		{"-C", testDir, "branch", "-d", "feat/x"},
		{"-C", testDir, "branch", "-D", "feat/x"},
	}
	calls := fake.Calls()
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i := range want {
		if got := calls[i].Args; !reflect.DeepEqual(got, want[i]) {
			t.Errorf("call %d args = %v, want %v", i, got, want[i])
		}
	}
}

func TestDeleteBranchDoesNotForceOtherFailures(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" branch -d feat/x", runner.Result{Stderr: "error: branch 'feat/x' not found\n"}, errors.New("exit status 1"))

	err := New(fake).DeleteBranch(context.Background(), testDir, "feat/x")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected git stderr in the error, got: %v", err)
	}
	if len(fake.Calls()) != 1 {
		t.Errorf("expected only the -d attempt, got: %+v", fake.Calls())
	}
}
