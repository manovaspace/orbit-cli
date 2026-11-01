// Package ghcli implements the CI command port with gh, sudo/systemctl and
// standard input over runner.Runner.
package ghcli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// StdinRunner runs a command with a string on its standard input. Secret values
// must never reach argv, so the adapter needs this beyond runner.Runner.
type StdinRunner interface {
	RunStdin(ctx context.Context, dir, stdin, name string, args ...string) (runner.Result, error)
}

// GH runs repository and service commands.
type GH struct {
	Runner runner.Runner
	Stdin  StdinRunner
}

// New builds a GH over r. Standard input goes through ExecStdin unless the
// caller swaps it for a test double.
func New(r runner.Runner) *GH {
	return &GH{Runner: r, Stdin: ExecStdin{}}
}

// Run executes name with args and returns its standard output. stdin is fed to
// the process when non-empty.
func (g *GH) Run(ctx context.Context, stdin, dir, name string, args ...string) (string, error) {
	var (
		res runner.Result
		err error
	)
	if stdin == "" {
		res, err = g.Runner.Run(ctx, dir, name, args...)
	} else {
		stdinRunner := g.Stdin
		if stdinRunner == nil {
			stdinRunner = ExecStdin{}
		}
		res, err = stdinRunner.RunStdin(ctx, dir, stdin, name, args...)
	}
	if err != nil {
		return res.Stdout, commandError(err, res)
	}
	return res.Stdout, nil
}

// ExecStdin runs one command with a string on standard input.
type ExecStdin struct{}

// RunStdin executes name with args in dir, feeding stdin.
func (ExecStdin) RunStdin(ctx context.Context, dir, stdin, name string, args ...string) (runner.Result, error) {
	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(stdin)
	if dir != "" {
		cmd.Dir = dir
	}

	line := strings.Join(append([]string{name}, args...), " ")
	runErr := cmd.Run()

	res := runner.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if runErr == nil {
		return res, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, fmt.Errorf("%s: %w", line, ctxErr)
	}
	if errors.Is(runErr, exec.ErrNotFound) {
		return res, clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("cannot run %s", name)))
	}
	if cmd.ProcessState != nil {
		return res, clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("%s exited with code %d", line, res.ExitCode)))
	}
	return res, clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("cannot run %s", line)))
}

// commandError keeps the runner's clierr code and prefers the command's own
// stderr text as the message.
func commandError(err error, res runner.Result) error {
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.Stdout)
	}
	if detail == "" {
		return err
	}
	return &clierr.Error{Code: clierr.Code(err), Exit: clierr.ExitCode(err), Message: detail}
}
