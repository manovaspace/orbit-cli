package stack_test

import (
	"context"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/stack"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestStartRunsDockerComposeInInfraDir(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("docker compose up -d", runner.Result{}, nil)

	if err := stack.New(fake).Start(context.Background(), "/workspace/orbit/orbit-infra"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want one", calls)
	}
	call := calls[0]
	if call.Dir != "/workspace/orbit/orbit-infra" {
		t.Errorf("dir = %q", call.Dir)
	}
	if call.Name != "docker" || strings.Join(call.Args, " ") != "compose up -d" {
		t.Errorf("command = %s %v", call.Name, call.Args)
	}
}

func TestStartPropagatesFailure(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("docker compose up -d", runner.Result{ExitCode: 1}, context.Canceled)

	if err := stack.New(fake).Start(context.Background(), "/infra"); err == nil {
		t.Fatal("expected the docker failure")
	}
}

func TestStartWithoutRunnerIsANoop(t *testing.T) {
	if err := stack.New(nil).Start(context.Background(), "/infra"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}
