// Package gitcli implements the workspace git port over a runner.Runner.
package gitcli

import (
	"context"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

// Git calls git through a runner.Runner.
type Git struct {
	Runner runner.Runner
}

// New builds a Git over r.
func New(r runner.Runner) *Git {
	return &Git{Runner: r}
}

func (g *Git) run(ctx context.Context, dir string, args ...string) (runner.Result, error) {
	if dir == "" {
		return g.Runner.Run(ctx, "", "git", args...)
	}
	return g.Runner.Run(ctx, dir, "git", append([]string{"-C", dir}, args...)...)
}

// IsWorkTree reports whether dir is inside a git working tree.
func (g *Git) IsWorkTree(ctx context.Context, dir string) (bool, error) {
	res, err := g.run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, &commandError{detail: combinedOutput(res), err: err}
	}
	return strings.TrimSpace(res.Stdout) == "true", nil
}

// Branch returns the current branch, falling back to the symbolic ref and then
// to "HEAD", matching the pre-refactor behaviour.
func (g *Git) Branch(ctx context.Context, dir string) (string, error) {
	if res, err := g.run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		return strings.TrimSpace(res.Stdout), nil
	}
	if res, err := g.run(ctx, dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(res.Stdout), nil
	}
	return "HEAD", nil
}

// Status returns the raw porcelain status.
func (g *Git) Status(ctx context.Context, dir string) (string, error) {
	res, err := g.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return "", &commandError{detail: combinedOutput(res), err: err}
	}
	return res.Stdout, nil
}

// AheadBehind compares HEAD with its upstream.
func (g *Git) AheadBehind(ctx context.Context, dir string) (int, int, error) {
	res, err := g.run(ctx, dir, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0, &commandError{detail: combinedOutput(res), err: err}
	}
	ahead, behind := domain.ParseAheadBehind(res.Stdout)
	return ahead, behind, nil
}

// Fetch fetches origin.
func (g *Git) Fetch(ctx context.Context, dir string) error {
	res, err := g.run(ctx, dir, "fetch", "origin")
	if err != nil {
		return &commandError{detail: combinedOutput(res), err: err}
	}
	return nil
}

// MergeFFOnly merges ref into the current branch without a merge commit.
func (g *Git) MergeFFOnly(ctx context.Context, dir, ref string) (string, error) {
	res, err := g.run(ctx, dir, "merge", "--ff-only", ref)
	return combinedOutput(res), err
}

// Clone clones remoteURL into dest, optionally from dir.
func (g *Git) Clone(ctx context.Context, dir, remoteURL, dest string) (string, error) {
	args := []string{"clone", "--", remoteURL, dest}
	var res runner.Result
	var err error
	if dir == "" {
		res, err = g.Runner.Run(ctx, "", "git", args...)
	} else {
		res, err = g.Runner.Run(ctx, dir, "git", append([]string{"-C", dir}, args...)...)
	}
	return combinedOutput(res), err
}

// commandError keeps the runner's clierr code while carrying git's own output
// as detail for messages that include it.
type commandError struct {
	detail string
	err    error
}

func (e *commandError) Error() string {
	if e.detail != "" {
		return e.detail
	}
	return e.err.Error()
}

func (e *commandError) Unwrap() error { return e.err }

func combinedOutput(res runner.Result) string {
	stdout := strings.TrimSpace(res.Stdout)
	stderr := strings.TrimSpace(res.Stderr)
	switch {
	case stdout != "" && stderr != "":
		return stdout + "\n" + stderr
	case stdout != "":
		return stdout
	default:
		return stderr
	}
}
