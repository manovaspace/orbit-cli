// Package runner is the seam through which commands invoke external programs.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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

// EnvRunner executes external commands with extra environment entries. Each
// env entry is KEY=VALUE and is added to, and overrides, the base environment.
type EnvRunner interface {
	RunEnv(ctx context.Context, dir string, env []string, name string, args ...string) (Result, error)
}

// Interactive executes external commands with the process's own stdin,
// stdout and stderr attached. Each env entry is KEY=VALUE and is added to, and
// overrides, the base environment.
type Interactive interface {
	RunInteractive(ctx context.Context, dir string, env []string, name string, args ...string) error
}

// Exec runs commands with os/exec. A non-nil Env replaces the process
// environment; nil inherits the caller's environment.
type Exec struct {
	Env []string
}

var (
	_ Runner      = Exec{}
	_ EnvRunner   = Exec{}
	_ Interactive = Exec{}
)

// Run executes name with args in dir. It returns a nil error only when the
// process started and exited 0. A missing binary or a nonzero exit returns a
// *clierr.Error with the tool code and exit code; a cancelled context returns
// ctx.Err() wrapped.
func (e Exec) Run(ctx context.Context, dir, name string, args ...string) (Result, error) {
	var stdout, stderr bytes.Buffer
	cmd := e.command(ctx, dir, name, args)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if e.Env != nil {
		cmd.Env = e.Env
	}

	runErr := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode(cmd)}
	return res, mapError(ctx, cmd, runErr, name, args, res.ExitCode)
}

// RunEnv is Run with env entries added to the base environment. The base is
// Exec.Env when non-nil, otherwise os.Environ(). Entries in env override base
// entries with the same key.
func (e Exec) RunEnv(ctx context.Context, dir string, env []string, name string, args ...string) (Result, error) {
	var stdout, stderr bytes.Buffer
	cmd := e.command(ctx, dir, name, args)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = mergeEnv(e.baseEnv(), env)

	runErr := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode(cmd)}
	return res, mapError(ctx, cmd, runErr, name, args, res.ExitCode)
}

// RunInteractive executes name with args in dir with the process's stdin,
// stdout and stderr attached. The environment rule matches RunEnv. Errors
// follow the same mapping as Run.
func (e Exec) RunInteractive(ctx context.Context, dir string, env []string, name string, args ...string) error {
	cmd := e.command(ctx, dir, name, args)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = mergeEnv(e.baseEnv(), env)

	runErr := cmd.Run()
	return mapError(ctx, cmd, runErr, name, args, exitCode(cmd))
}

func (e Exec) command(ctx context.Context, dir, name string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd
}

func (e Exec) baseEnv() []string {
	if e.Env != nil {
		return e.Env
	}
	return os.Environ()
}

// mergeEnv returns base with every entry whose key appears in extra removed,
// followed by extra. When extra repeats a key, the last entry wins.
func mergeEnv(base, extra []string) []string {
	last := make(map[string]int, len(extra))
	for i, kv := range extra {
		last[envKey(kv)] = i
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		if _, ok := last[envKey(kv)]; !ok {
			out = append(out, kv)
		}
	}
	for i, kv := range extra {
		if last[envKey(kv)] == i {
			out = append(out, kv)
		}
	}
	return out
}

func envKey(kv string) string {
	key, _, _ := strings.Cut(kv, "=")
	return key
}

func exitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return 0
}

// mapError converts the outcome of a finished command into the error contract
// shared by Run, RunEnv and RunInteractive.
func mapError(ctx context.Context, cmd *exec.Cmd, runErr error, name string, args []string, code int) error {
	if runErr == nil {
		return nil
	}
	line := strings.Join(append([]string{name}, args...), " ")
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", line, ctxErr)
	}
	if errors.Is(runErr, exec.ErrNotFound) {
		return clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("cannot run %s", name)))
	}
	if cmd.ProcessState != nil {
		return clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("%s exited with code %d", line, code)))
	}
	return clierr.Wrap(runErr, clierr.Tool(fmt.Sprintf("cannot run %s", line)))
}
