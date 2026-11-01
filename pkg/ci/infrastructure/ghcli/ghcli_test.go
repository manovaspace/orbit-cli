package ghcli

import (
	"context"
	"reflect"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

type stdinCall struct {
	stdin string
	dir   string
	name  string
	args  []string
}

// fakeStdin records commands that carry standard input.
type fakeStdin struct {
	calls []stdinCall
	out   string
}

func (f *fakeStdin) RunStdin(_ context.Context, dir, stdin, name string, args ...string) (runner.Result, error) {
	f.calls = append(f.calls, stdinCall{stdin: stdin, dir: dir, name: name, args: args})
	return runner.Result{Stdout: f.out}, nil
}

func TestRunUsesRunnerWithoutStdin(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh variable get", runner.Result{Stdout: "vps\n"}, nil)

	out, err := New(fake).Run(context.Background(), "", "", "gh", "variable", "get", "CI_RUNNER", "-R", "manovaspace/a")
	if err != nil {
		t.Fatal(err)
	}
	if out != "vps\n" {
		t.Errorf("out = %q", out)
	}
	want := []string{"variable", "get", "CI_RUNNER", "-R", "manovaspace/a"}
	if calls := fake.Calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("calls = %+v, want %v", calls, want)
	}
}

func TestRunRoutesStdinToStdinRunner(t *testing.T) {
	fake := &runnertest.Fake{}
	stdin := &fakeStdin{out: ""}
	gh := &GH{Runner: fake, Stdin: stdin}

	if _, err := gh.Run(context.Background(), "s3cret", "", "gh", "secret", "set", "NPM_TOKEN", "-R", "manovaspace/a"); err != nil {
		t.Fatal(err)
	}
	if len(stdin.calls) != 1 {
		t.Fatalf("stdin calls = %+v", stdin.calls)
	}
	if stdin.calls[0].stdin != "s3cret" {
		t.Errorf("stdin = %q", stdin.calls[0].stdin)
	}
	want := []string{"secret", "set", "NPM_TOKEN", "-R", "manovaspace/a"}
	if !reflect.DeepEqual(stdin.calls[0].args, want) {
		t.Errorf("args = %v, want %v", stdin.calls[0].args, want)
	}
	if len(fake.Calls()) != 0 {
		t.Errorf("runner called for a stdin command: %+v", fake.Calls())
	}
}

func TestRunKeepsStderrInErrors(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("gh variable delete", runner.Result{Stderr: "variable not found\n"}, context.DeadlineExceeded)

	_, err := New(fake).Run(context.Background(), "", "", "gh", "variable", "delete", "CI_RUNNER", "-R", "manovaspace/a")
	if err == nil || err.Error() != "variable not found" {
		t.Fatalf("err = %v", err)
	}
}
