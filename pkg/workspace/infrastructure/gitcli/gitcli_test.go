package gitcli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestIsWorkTreeCommand(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo rev-parse --is-inside-work-tree", runner.Result{Stdout: "true\n"}, nil)

	inside, err := New(fake).IsWorkTree(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("IsWorkTree: %v", err)
	}
	if !inside {
		t.Error("expected inside=true")
	}
	call := fake.Calls()[0]
	if call.Dir != "/repo" || call.Name != "git" {
		t.Errorf("call = %+v", call)
	}
	want := []string{"-C", "/repo", "rev-parse", "--is-inside-work-tree"}
	if !reflect.DeepEqual(call.Args, want) {
		t.Errorf("args = %v, want %v", call.Args, want)
	}
}

func TestBranchFallsBackToSymbolicRef(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo rev-parse --abbrev-ref HEAD", runner.Result{}, errors.New("detached"))
	fake.On("git -C /repo symbolic-ref --short HEAD", runner.Result{Stdout: "main\n"}, nil)

	branch, err := New(fake).Branch(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
	if len(fake.Calls()) != 2 {
		t.Errorf("calls = %v", fake.Calls())
	}
}

func TestBranchFallsBackToHead(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo rev-parse --abbrev-ref HEAD", runner.Result{}, errors.New("detached"))
	fake.On("git -C /repo symbolic-ref --short HEAD", runner.Result{}, errors.New("detached"))

	branch, err := New(fake).Branch(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if branch != "HEAD" {
		t.Errorf("branch = %q, want HEAD", branch)
	}
}

func TestStatusCommand(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo status --porcelain", runner.Result{Stdout: " M file.txt\n"}, nil)

	out, err := New(fake).Status(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if out != " M file.txt\n" {
		t.Errorf("status = %q", out)
	}
}

func TestAheadBehindCommand(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo rev-list --left-right --count HEAD...@{u}", runner.Result{Stdout: "2\t3\n"}, nil)

	ahead, behind, err := New(fake).AheadBehind(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("AheadBehind: %v", err)
	}
	if ahead != 2 || behind != 3 {
		t.Errorf("counts = %d/%d", ahead, behind)
	}
}

func TestFetchCommand(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo fetch origin", runner.Result{}, nil)
	if err := New(fake).Fetch(context.Background(), "/repo"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestMergeFFOnlyCommand(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo merge --ff-only origin/main", runner.Result{Stdout: "Updating abc..def\nFast-forward\n"}, nil)

	out, err := New(fake).MergeFFOnly(context.Background(), "/repo", "origin/main")
	if err != nil {
		t.Fatalf("MergeFFOnly: %v", err)
	}
	if out == "" {
		t.Error("expected merge output")
	}
}

func TestCloneCommands(t *testing.T) {
	t.Run("from workspace root", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("git -C /ws clone -- https://example.com/repo.git orbit/repo", runner.Result{}, nil)
		if _, err := New(fake).Clone(context.Background(), "/ws", "https://example.com/repo.git", "orbit/repo"); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		call := fake.Calls()[0]
		want := []string{"-C", "/ws", "clone", "--", "https://example.com/repo.git", "orbit/repo"}
		if !reflect.DeepEqual(call.Args, want) {
			t.Errorf("args = %v, want %v", call.Args, want)
		}
	})

	t.Run("absolute temporary clone", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("git clone -- https://example.com/repo.git /tmp/clone", runner.Result{}, nil)
		if _, err := New(fake).Clone(context.Background(), "", "https://example.com/repo.git", "/tmp/clone"); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		call := fake.Calls()[0]
		if call.Dir != "" {
			t.Errorf("Dir = %q, want empty", call.Dir)
		}
		want := []string{"clone", "--", "https://example.com/repo.git", "/tmp/clone"}
		if !reflect.DeepEqual(call.Args, want) {
			t.Errorf("args = %v, want %v", call.Args, want)
		}
	})
}

func TestCommandErrorKeepsDetail(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C /repo status --porcelain", runner.Result{Stderr: "fatal: nope"}, errors.New("exit 128"))

	_, err := New(fake).Status(context.Background(), "/repo")
	if err == nil || err.Error() != "fatal: nope" {
		t.Fatalf("error = %v", err)
	}
}
