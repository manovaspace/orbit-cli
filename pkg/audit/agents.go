package audit

import (
	"context"
	"os/exec"

	"github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// runnerCommands adapts runner.Runner to the audit command port and resolves
// editor binaries from PATH.
type runnerCommands struct {
	runner runner.Runner
}

func (c runnerCommands) Run(ctx context.Context, dir, name string, args ...string) (application.CommandResult, error) {
	res, err := c.runner.Run(ctx, dir, name, args...)
	return application.CommandResult{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, err
}

func (runnerCommands) LookPath(name string) (string, error) { return exec.LookPath(name) }
