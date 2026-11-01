// Package runner is the seam through which commands invoke external programs.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// Result is the outcome of one external command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner executes external commands.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) (Result, error)
}

// Exec runs commands with os/exec. A non-nil Env replaces the process
// environment; nil inherits the caller's environment.
type Exec struct {
	Env []string
}

// Run executes name with args in dir. It returns a nil error only when the
// process started and exited 0. A missing binary or a nonzero exit returns a
// *clierr.Error with the tool code and exit code; a cancelled context returns
// ctx.Err() wrapped.
func (e Exec) Run(ctx context.Context, dir, name string, args ...string) (Result, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if dir != "" {
		cmd.Dir = dir
	}
	if e.Env != nil {
		cmd.Env = e.Env
	}

	line := strings.Join(append([]string{name}, args...), " ")
	runErr := cmd.Run()

	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
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
