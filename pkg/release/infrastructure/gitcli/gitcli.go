// Package gitcli implements the release git port over a runner.Runner.
package gitcli

import (
	"context"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Git calls `git -C <dir>` through a runner.Runner.
type Git struct {
	Runner runner.Runner
}

// New builds a Git over r.
func New(r runner.Runner) *Git { return &Git{Runner: r} }

func (g *Git) run(ctx context.Context, dir string, args ...string) (runner.Result, error) {
	if dir == "" {
		dir = "."
	}
	return g.Runner.Run(ctx, dir, "git", append([]string{"-C", dir}, args...)...)
}

// RevParse resolves ref to a commit SHA.
func (g *Git) RevParse(ctx context.Context, dir, ref string) (string, error) {
	res, err := g.run(ctx, dir, "rev-parse", ref)
	if err != nil {
		return "", gitError(err, res)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// LatestTag returns the highest version tag, or an empty string when the
// repository has no tags.
func (g *Git) LatestTag(ctx context.Context, dir string) (string, error) {
	res, err := g.run(ctx, dir, "tag", "--list", "--sort=-v:refname")
	if err != nil {
		return "", gitError(err, res)
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if tag := strings.TrimSpace(line); tag != "" {
			return tag, nil
		}
	}
	return "", nil
}

// gitError keeps the runner's clierr code and prefers git's own stderr text.
func gitError(err error, res runner.Result) error {
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.Stdout)
	}
	if detail == "" {
		return err
	}
	return &clierr.Error{Code: clierr.Code(err), Exit: clierr.ExitCode(err), Message: detail}
}
