package execprobe

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestRunUsesExactCommandAndCombinesOutput(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("git version", runner.Result{Stdout: "git version 2.50.0", Stderr: "warning"}, nil)

	res, err := New(fake).Run(context.Background(), application.DefaultCommandTimeout, "git", "version")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res, application.ProbeResult{Output: "git version 2.50.0warning"}) {
		t.Errorf("result = %+v", res)
	}
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Name != "git" || !reflect.DeepEqual(calls[0].Args, []string{"version"}) {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Dir != "" {
		t.Errorf("dir = %q, want process default", calls[0].Dir)
	}
}

func TestRunPropagatesProbeFailure(t *testing.T) {
	fake := &runnertest.Fake{}
	wantErr := errors.New("missing binary")
	fake.On("docker info", runner.Result{Stderr: "cannot connect"}, wantErr)

	res, err := New(fake).Run(context.Background(), time.Second, "docker", "info")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if res.Output != "cannot connect" {
		t.Errorf("output = %q", res.Output)
	}
}
