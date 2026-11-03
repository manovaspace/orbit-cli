// Package gitcli implements the pull-request git port over a runner.Runner.
package gitcli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

const repoOriginHint = "run inside a repository, pass --dir <repo>, or give a name pattern to pr status"

// Git calls `git -C <dir>` through a runner.Runner.
type Git struct {
	Runner runner.Runner
}

// New builds a Git over r.
func New(r runner.Runner) *Git {
	return &Git{Runner: r}
}

func (g *Git) run(ctx context.Context, dir string, args ...string) (runner.Result, error) {
	if dir == "" {
		dir = "."
	}
	return g.Runner.Run(ctx, dir, "git", append([]string{"-C", dir}, args...)...)
}

// Repo detects the remote origin metadata and the active branch of dir.
func (g *Git) Repo(ctx context.Context, dir string) (domain.RepoInfo, error) {
	if dir == "" {
		dir = "."
	}

	res, err := g.run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		fallback, fallbackErr := g.run(ctx, dir, "remote", "get-url", "origin")
		if fallbackErr != nil {
			return domain.RepoInfo{}, repoOriginError(dir, fallback, fallbackErr)
		}
		res = fallback
	}
	remoteURL := strings.TrimSpace(res.Stdout)
	if remoteURL == "" {
		return domain.RepoInfo{}, fmt.Errorf("empty remote origin URL in %s", dir)
	}

	owner, name, err := domain.ParseRemoteURL(remoteURL)
	if err != nil {
		return domain.RepoInfo{}, err
	}

	branchRes, err := g.run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return domain.RepoInfo{}, fmt.Errorf("failed to get current git branch in %s: %w", dir, err)
	}
	branch := strings.TrimSpace(branchRes.Stdout)
	if branch == "HEAD" {
		if sym, symErr := g.run(ctx, dir, "symbolic-ref", "--short", "HEAD"); symErr == nil {
			branch = strings.TrimSpace(sym.Stdout)
		}
	}

	return domain.RepoInfo{
		Repo:          domain.Repo{Owner: owner, Name: name},
		CurrentBranch: branch,
		RemoteURL:     remoteURL,
		IsGitHub:      strings.Contains(strings.ToLower(remoteURL), "github.com"),
	}, nil
}

// repoOriginError maps a failed origin lookup to a clierr error with a hint.
// A directory that is not a repository is a usage error, a missing git binary
// keeps the tool exit code, and every other origin failure keeps the tool class
// when the runner already reported one. The cause stays wrapped for errors.Is
// and errors.As but is not rendered in the user-facing message.
func repoOriginError(dir string, res runner.Result, cause error) error {
	msg := fmt.Sprintf("not a git repository with an origin remote: %s", dir)

	var mapped *clierr.Error
	switch {
	case errors.Is(cause, exec.ErrNotFound):
		mapped = clierr.Tool(msg)
	case isNotARepository(res, cause):
		mapped = clierr.Usage(msg)
	case clierr.Code(cause) == clierr.CodeTool:
		mapped = clierr.Tool(msg)
	default:
		mapped = clierr.Failed(msg)
	}
	return clierr.WithHint(clierr.WrapClean(cause, mapped), repoOriginHint)
}

func isNotARepository(res runner.Result, cause error) bool {
	text := res.Stderr + "\n" + res.Stdout
	if cause != nil {
		text += "\n" + cause.Error()
	}
	return strings.Contains(text, "not a git repository")
}

// IsClean reports whether the working tree has no uncommitted changes.
func (g *Git) IsClean(ctx context.Context, dir string) (bool, error) {
	res, err := g.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, gitError(err, res)
	}
	return strings.TrimSpace(res.Stdout) == "", nil
}

// LastCommit returns the subject and body of the last commit on branch.
func (g *Git) LastCommit(ctx context.Context, dir, branch string) (subject, body string, err error) {
	subjectRes, err := g.run(ctx, dir, "log", "-1", "--pretty=%s", branch)
	if err != nil {
		return "", "", gitError(err, subjectRes)
	}
	bodyRes, err := g.run(ctx, dir, "log", "-1", "--pretty=%b", branch)
	if err != nil {
		return "", "", gitError(err, bodyRes)
	}
	return strings.TrimSpace(subjectRes.Stdout), strings.TrimSpace(bodyRes.Stdout), nil
}

// Push pushes branch to origin and sets its upstream.
func (g *Git) Push(ctx context.Context, dir, branch string) error {
	res, err := g.run(ctx, dir, "push", "-u", "origin", branch)
	if err != nil {
		return gitError(err, res)
	}
	return nil
}

// Checkout switches the working tree to branch.
func (g *Git) Checkout(ctx context.Context, dir, branch string) error {
	res, err := g.run(ctx, dir, "checkout", branch)
	if err != nil {
		return gitError(err, res)
	}
	return nil
}

// PullFFOnly fast-forwards the current branch from its upstream.
func (g *Git) PullFFOnly(ctx context.Context, dir string) error {
	res, err := g.run(ctx, dir, "pull", "--ff-only")
	if err != nil {
		return gitError(err, res)
	}
	return nil
}

// DeleteBranch runs `branch -d`, falling back to `-D` when git says the branch
// is not fully merged.
func (g *Git) DeleteBranch(ctx context.Context, dir, branch string) error {
	res, err := g.run(ctx, dir, "branch", "-d", branch)
	if err == nil {
		return nil
	}
	if strings.Contains(res.Stdout, "not fully merged") || strings.Contains(res.Stderr, "not fully merged") {
		forceRes, forceErr := g.run(ctx, dir, "branch", "-D", branch)
		if forceErr != nil {
			return gitError(forceErr, forceRes)
		}
		return nil
	}
	return gitError(err, res)
}

// gitError keeps the runner's clierr code and prefers git's own stderr text as
// the message.
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
