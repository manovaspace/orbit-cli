// Package attached runs a foreground command with the caller's terminal
// attached (stdin, stdout and stderr), for `orbit dev tunnel`.
package attached

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// Runner runs argv in the foreground with the given streams attached.
type Runner struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// New builds a Runner over the given streams.
func New(in io.Reader, out, errOut io.Writer) *Runner {
	return &Runner{In: in, Out: out, Err: errOut}
}

// Run starts argv[0] with argv[1:] and waits for it. A process ended by SIGINT
// (Ctrl-C) is a normal stop and returns nil. A nonzero exit returns a tool
// error with the exit code; a missing binary is a tool error too.
func (r *Runner) Run(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return clierr.Usage("no command to run")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = r.In
	cmd.Stdout = r.Out
	cmd.Stderr = r.Err

	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", argv[0], ctxErr)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return clierr.Wrap(err, clierr.Tool(fmt.Sprintf("cannot run %s; install OpenSSH", argv[0])))
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() && status.Signal() == syscall.SIGINT {
			return nil
		}
		return clierr.Wrap(err, clierr.Tool(fmt.Sprintf("%s exited with code %d", argv[0], exitErr.ExitCode())))
	}
	return clierr.Wrap(err, clierr.Tool(fmt.Sprintf("cannot run %s", argv[0])))
}
