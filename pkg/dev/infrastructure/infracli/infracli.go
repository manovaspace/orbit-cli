// Package infracli implements the dev command port. Run goes through
// runner.Runner; Stream attaches the process to the terminal for logs.
package infracli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Commands runs the orbit-infra scripts and docker compose.
type Commands struct {
	Runner runner.Runner
	Out    io.Writer
	ErrOut io.Writer
	Stdin  io.Reader
}

// New builds Commands over r, writing captured output to out/errOut.
func New(r runner.Runner, out, errOut io.Writer) *Commands {
	return &Commands{Runner: r, Out: out, ErrOut: errOut, Stdin: os.Stdin}
}

// Run executes a finite command and forwards its captured output.
func (c *Commands) Run(ctx context.Context, dir, name string, args ...string) error {
	res, err := c.Runner.Run(ctx, dir, name, args...)
	if c.Out != nil && res.Stdout != "" {
		_, _ = io.WriteString(c.Out, res.Stdout)
	}
	if c.ErrOut != nil && res.Stderr != "" {
		_, _ = io.WriteString(c.ErrOut, res.Stderr)
	}
	return err
}

// Stream attaches the process to the terminal. runner.Runner buffers output,
// which would hide an unbounded stream (logs -f), so this path uses os/exec.
func (c *Commands) Stream(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = c.Stdin
	cmd.Stdout = c.Out
	cmd.Stderr = c.ErrOut
	cmd.Env = os.Environ()

	line := strings.Join(append([]string{name}, args...), " ")
	runErr := cmd.Run()
	if runErr == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", line, ctxErr)
	}
	if errors.Is(runErr, exec.ErrNotFound) {
		return clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("cannot run %s", name)))
	}
	if cmd.ProcessState != nil {
		return clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("%s exited with code %d", line, cmd.ProcessState.ExitCode())))
	}
	return clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("cannot run %s", line)))
}
