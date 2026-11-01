package infracli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestRunExecutesExactCommand(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("/infra/compose.sh up -d", runner.Result{Stdout: "started\n"}, nil)

	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	err := New(fake, out, errOut).Run(context.Background(), "/infra", "/infra/compose.sh", "up", "-d")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Dir != "/infra" || calls[0].Name != "/infra/compose.sh" {
		t.Errorf("unexpected call: %+v", calls[0])
	}
	if !reflect.DeepEqual(calls[0].Args, []string{"up", "-d"}) {
		t.Errorf("args = %v", calls[0].Args)
	}
	if out.String() != "started\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestRunForwardsStderrAndError(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("docker compose down", runner.Result{Stderr: "boom\n"}, clierr.Tool("docker exited with code 1"))

	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	err := New(fake, out, errOut).Run(context.Background(), "/infra", "docker", "compose", "down")
	if err == nil {
		t.Fatal("expected an error")
	}
	if errOut.String() != "boom\n" {
		t.Errorf("stderr = %q", errOut.String())
	}
	if clierr.Code(err) != clierr.CodeTool {
		t.Errorf("code = %q", clierr.Code(err))
	}
}

func TestStreamMissingBinaryIsToolError(t *testing.T) {
	err := New(&runnertest.Fake{}, new(bytes.Buffer), new(bytes.Buffer)).Stream(context.Background(), t.TempDir(), "orbit-no-such-command")
	if err == nil {
		t.Fatal("expected an error")
	}
	if clierr.Code(err) != clierr.CodeTool {
		t.Errorf("code = %q, want %q", clierr.Code(err), clierr.CodeTool)
	}
	if !strings.Contains(err.Error(), "cannot run orbit-no-such-command") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStreamPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := New(&runnertest.Fake{}, new(bytes.Buffer), new(bytes.Buffer)).Stream(ctx, t.TempDir(), "sh", "-c", "sleep 5")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation, got %v", err)
	}
}
