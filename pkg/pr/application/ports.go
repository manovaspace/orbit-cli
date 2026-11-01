// Package application implements the pull-request use cases over the forge and
// git ports, keeping the CLI commands thin and the logic testable without
// external binaries.
package application

import (
	"context"
	"io"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

// Forge is the remote host that owns the pull requests.
type Forge interface {
	CreatePR(ctx context.Context, repo domain.Repo, title, body, head, base string) (domain.PullRequest, error)
	MergePR(ctx context.Context, repo domain.Repo, ref string, method domain.Method) error
	// Checks returns an empty slice (nil error) when the forge says "no checks
	// reported", and an error when checks cannot be read.
	Checks(ctx context.Context, repo domain.Repo, ref string) ([]domain.Check, error)
	Comment(ctx context.Context, repo domain.Repo, ref, body string) error
	ListOpen(ctx context.Context, repo domain.Repo) ([]domain.PullRequest, error)
}

// Git is the local checkout the use cases push, fetch and prune.
type Git interface {
	Repo(ctx context.Context, dir string) (domain.RepoInfo, error)
	IsClean(ctx context.Context, dir string) (bool, error)
	LastCommit(ctx context.Context, dir, branch string) (subject, body string, err error)
	Push(ctx context.Context, dir, branch string) error
	Checkout(ctx context.Context, dir, branch string) error
	PullFFOnly(ctx context.Context, dir string) error
	// DeleteBranch runs `branch -d`, falling back to `-D` when git reports the
	// branch is not fully merged.
	DeleteBranch(ctx context.Context, dir, branch string) error
}

// UI carries the decoration cmd/orbit already renders. Keeping it injected lets
// this package write the same progress lines without importing a styling
// library.
type UI struct {
	Arrow string
	OK    string
	Warn  string
	Title func(string) string
	Bold  func(string) string
}

// Service is the pull-request use-case layer.
type Service struct {
	Forge Forge
	Git   Git
	Out   io.Writer
	Sleep func(time.Duration)
	Now   func() time.Time
	// PollInterval and RegisterWindow default to 10s and 60s.
	PollInterval, RegisterWindow time.Duration
	UI                           UI
}
