package gitcli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

const testDir = "/repo"

func TestRevParseArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" rev-parse origin/main", runner.Result{Stdout: "abc123\n"}, nil)

	sha, err := New(fake).RevParse(context.Background(), testDir, "origin/main")
	if err != nil || sha != "abc123" {
		t.Fatalf("sha = %q err = %v", sha, err)
	}
	want := []string{"-C", testDir, "rev-parse", "origin/main"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestLatestTagArgs(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" tag --list", runner.Result{Stdout: "v1.3.0\nv1.2.0\n"}, nil)

	tag, err := New(fake).LatestTag(context.Background(), testDir)
	if err != nil || tag != "v1.3.0" {
		t.Fatalf("tag = %q err = %v", tag, err)
	}
	want := []string{"-C", testDir, "tag", "--list", "--sort=-v:refname"}
	if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestLatestTagEmpty(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" tag --list", runner.Result{}, nil)

	tag, err := New(fake).LatestTag(context.Background(), testDir)
	if err != nil || tag != "" {
		t.Fatalf("tag = %q err = %v", tag, err)
	}
}

func TestRevParseKeepsGitStderr(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git -C "+testDir+" rev-parse origin/main", runner.Result{Stderr: "unknown revision\n"}, errors.New("exit status 128"))

	_, err := New(fake).RevParse(context.Background(), testDir, "origin/main")
	if err == nil || err.Error() != "unknown revision" {
		t.Fatalf("err = %v", err)
	}
}
