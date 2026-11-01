package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

// Timings used when the caller leaves the gate defaults unset.
const (
	DefaultPollInterval   = 10 * time.Second
	DefaultRegisterWindow = 60 * time.Second
	DefaultWaitTimeout    = 20 * time.Minute
)

// CreateInput is one `pr create` invocation.
type CreateInput struct {
	Title  string
	Body   string
	Base   string
	Head   string
	Merge  bool
	Method domain.Method
	Gate   domain.MergeOptions
}

// CreateResult is what `pr create` produced.
type CreateResult struct {
	Repo        domain.Repo        `json:"repo"`
	Branch      string             `json:"branch"`
	Base        string             `json:"base"`
	PullRequest domain.PullRequest `json:"pullRequest"`
	Merged      bool               `json:"merged"`
}

// MergeResult is what `pr merge` produced.
type MergeResult struct {
	Repo   domain.Repo   `json:"repo"`
	Ref    string        `json:"ref"`
	Method domain.Method `json:"method"`
}

// DeliverInput is one `pr deliver` invocation.
type DeliverInput struct {
	Title  string
	Body   string
	Base   string
	Method domain.Method
	Gate   domain.MergeOptions
}

// DeliverResult is what `pr deliver` produced.
type DeliverResult struct {
	Repo        domain.Repo        `json:"repo"`
	Branch      string             `json:"branch"`
	Base        string             `json:"base"`
	PullRequest domain.PullRequest `json:"pullRequest"`
}

// StatusResult lists the open pull requests of one repository.
type StatusResult struct {
	Repo         domain.Repo          `json:"repo"`
	PullRequests []domain.PullRequest `json:"pullRequests"`
}

// Create pushes the head branch, opens a pull request and optionally merges it
// through the merge gate.
func (s *Service) Create(ctx context.Context, dir string, in CreateInput) (CreateResult, error) {
	info, err := s.Git.Repo(ctx, dir)
	if err != nil {
		return CreateResult{}, err
	}

	head := strings.TrimSpace(in.Head)
	if head == "" {
		head = info.CurrentBranch
	}
	if head == "" || head == "HEAD" {
		return CreateResult{}, clierr.Failed("cannot determine head branch; specify --head or checkout a branch")
	}

	base := strings.TrimSpace(in.Base)
	if base == "" {
		base = "main"
	}
	if head == base {
		return CreateResult{}, clierr.Failed(fmt.Sprintf("head branch %q cannot be the same as base branch %q", head, base))
	}

	title, body, err := s.commitMessages(ctx, dir, head, in.Title, in.Body)
	if err != nil {
		return CreateResult{}, err
	}

	fmt.Fprintf(s.out(), "%s Pushing branch %s to origin...\n", s.icon(s.UI.Arrow), s.bold(head))
	if err := s.Git.Push(ctx, dir, head); err != nil {
		return CreateResult{}, withMessage(err, fmt.Sprintf("failed to push branch %q to origin", head))
	}
	fmt.Fprintf(s.out(), "  %s Pushed branch %s to origin\n", s.icon(s.UI.OK), s.bold(head))

	fmt.Fprintf(s.out(), "%s Creating pull request on GitHub (%s -> %s) in %s...\n",
		s.icon(s.UI.Arrow), s.bold(head), s.bold(base), info.Repo)
	created, err := s.Forge.CreatePR(ctx, info.Repo, title, body, head, base)
	if err != nil {
		return CreateResult{}, withMessage(err, "failed to create GitHub PR")
	}
	fmt.Fprintf(s.out(), "%s Pull request created: %s\n", s.icon(s.UI.OK), s.bold(created.URL))

	result := CreateResult{Repo: info.Repo, Branch: head, Base: base, PullRequest: created}
	if in.Merge {
		label := "#" + strconv.FormatInt(created.Number, 10)
		fmt.Fprintf(s.out(), "%s Merging pull request %s (%s)...\n", s.icon(s.UI.Arrow), label, in.Method)
		if err := s.mergeGated(ctx, info.Repo, strconv.FormatInt(created.Number, 10), label, in.Method, in.Gate); err != nil {
			return CreateResult{}, err
		}
		fmt.Fprintf(s.out(), "%s Pull request %s merged into %s (%s)\n", s.icon(s.UI.OK), label, s.bold(base), in.Method)
		result.Merged = true
	}
	return result, nil
}

// Merge merges one pull request after the merge gate allows it. A blank ref
// means the current branch.
func (s *Service) Merge(ctx context.Context, dir, ref string, method domain.Method, opts domain.MergeOptions) (MergeResult, error) {
	info, err := s.Git.Repo(ctx, dir)
	if err != nil {
		return MergeResult{}, err
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = info.CurrentBranch
		if ref == "" || ref == "HEAD" {
			return MergeResult{}, clierr.Failed("cannot determine current branch; provide PR number explicitly: orbit pr merge <number>")
		}
	}

	fmt.Fprintf(s.out(), "%s Merging pull request %s in %s on GitHub (%s)...\n", s.icon(s.UI.Arrow), ref, info.Repo, method)
	if err := s.mergeGated(ctx, info.Repo, ref, ref, method, opts); err != nil {
		return MergeResult{}, err
	}
	fmt.Fprintf(s.out(), "%s Pull request %s merged using method %q\n", s.icon(s.UI.OK), ref, method)
	return MergeResult{Repo: info.Repo, Ref: ref, Method: method}, nil
}

// mergeGated enforces the merge gate and merges ref. label names the pull
// request in error messages.
func (s *Service) mergeGated(ctx context.Context, repo domain.Repo, ref, label string, method domain.Method, opts domain.MergeOptions) error {
	if err := s.enforceGate(ctx, repo, ref, opts); err != nil {
		return err
	}
	if err := s.Forge.MergePR(ctx, repo, ref, method); err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("failed to merge pull request %s on GitHub", label)))
	}
	return nil
}

// Deliver pushes the current branch, opens a pull request, merges it through
// the gate, checks out the base branch, fast-forwards it and prunes the branch.
func (s *Service) Deliver(ctx context.Context, dir string, in DeliverInput) (DeliverResult, error) {
	info, err := s.Git.Repo(ctx, dir)
	if err != nil {
		return DeliverResult{}, err
	}

	base := strings.TrimSpace(in.Base)
	if base == "" {
		base = "main"
	}
	branch := info.CurrentBranch
	if branch == base {
		return DeliverResult{}, clierr.Failed(fmt.Sprintf("cannot deliver from %q branch: deliver must be run from a feature branch", base))
	}
	if branch == "" || branch == "HEAD" {
		return DeliverResult{}, clierr.Failed("cannot deliver from detached HEAD state; checkout a feature branch first")
	}

	clean, err := s.Git.IsClean(ctx, dir)
	if err != nil {
		return DeliverResult{}, withMessage(err, "checking git status")
	}
	if !clean {
		return DeliverResult{}, clierr.Failed("working tree has uncommitted changes; please commit or stash them before delivering")
	}

	title, body, err := s.commitMessages(ctx, dir, branch, in.Title, in.Body)
	if err != nil {
		return DeliverResult{}, err
	}

	fmt.Fprintln(s.out(), s.title(fmt.Sprintf("Orbit Deliver (GitHub) — %s (%s -> %s)", info.Repo, branch, base)))

	fmt.Fprintf(s.out(), "%s Pushing branch %s to origin...\n", s.icon(s.UI.Arrow), s.bold(branch))
	if err := s.Git.Push(ctx, dir, branch); err != nil {
		return DeliverResult{}, withMessage(err, fmt.Sprintf("failed to push branch %q to origin", branch))
	}
	fmt.Fprintf(s.out(), "  %s Pushed branch %s to origin\n", s.icon(s.UI.OK), s.bold(branch))

	fmt.Fprintf(s.out(), "%s Creating pull request %s -> %s...\n", s.icon(s.UI.Arrow), s.bold(branch), s.bold(base))
	created, err := s.Forge.CreatePR(ctx, info.Repo, title, body, branch, base)
	if err != nil {
		return DeliverResult{}, withMessage(err, "failed to create GitHub PR")
	}
	fmt.Fprintf(s.out(), "  %s Created PR #%d: %s\n", s.icon(s.UI.OK), created.Number, s.bold(created.URL))

	fmt.Fprintf(s.out(), "%s Merging pull request #%d (%s)...\n", s.icon(s.UI.Arrow), created.Number, in.Method)
	number := strconv.FormatInt(created.Number, 10)
	if err := s.mergeGated(ctx, info.Repo, number, "#"+number, in.Method, in.Gate); err != nil {
		return DeliverResult{}, err
	}
	fmt.Fprintf(s.out(), "  %s Merged PR #%d into %s\n", s.icon(s.UI.OK), created.Number, s.bold(base))

	fmt.Fprintf(s.out(), "%s Checking out %s...\n", s.icon(s.UI.Arrow), s.bold(base))
	if err := s.Git.Checkout(ctx, dir, base); err != nil {
		return DeliverResult{}, withMessage(err, fmt.Sprintf("failed to checkout %s", base))
	}
	fmt.Fprintf(s.out(), "  %s Switched to branch %s\n", s.icon(s.UI.OK), s.bold(base))

	fmt.Fprintf(s.out(), "%s Fast-forwarding %s to origin/%s...\n", s.icon(s.UI.Arrow), s.bold(base), base)
	if err := s.Git.PullFFOnly(ctx, dir); err != nil {
		return DeliverResult{}, withMessage(err, fmt.Sprintf("failed to pull --ff-only on %s", base))
	}
	fmt.Fprintf(s.out(), "  %s Fast-forwarded %s to origin/%s\n", s.icon(s.UI.OK), s.bold(base), base)

	fmt.Fprintf(s.out(), "%s Pruning local branch %s...\n", s.icon(s.UI.Arrow), s.bold(branch))
	if err := s.Git.DeleteBranch(ctx, dir, branch); err != nil {
		return DeliverResult{}, withMessage(err, fmt.Sprintf("failed to prune local branch %q", branch))
	}
	fmt.Fprintf(s.out(), "  %s Pruned local branch %s\n", s.icon(s.UI.OK), s.bold(branch))

	fmt.Fprintf(s.out(), "\n%s Successfully delivered %s into %s!\n", s.icon(s.UI.OK), s.bold(branch), s.bold(base))
	return DeliverResult{Repo: info.Repo, Branch: branch, Base: base, PullRequest: created}, nil
}

// Status lists the open pull requests of the repository in dir.
func (s *Service) Status(ctx context.Context, dir string) (StatusResult, error) {
	info, err := s.Git.Repo(ctx, dir)
	if err != nil {
		return StatusResult{}, err
	}
	open, err := s.Forge.ListOpen(ctx, info.Repo)
	if err != nil {
		return StatusResult{}, withMessage(err, "listing open GitHub PRs")
	}
	if open == nil {
		open = []domain.PullRequest{}
	}
	return StatusResult{Repo: info.Repo, PullRequests: open}, nil
}

// commitMessages fills the title and body from the last commit when the flags
// left them empty.
func (s *Service) commitMessages(ctx context.Context, dir, rev, title, body string) (string, string, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || body == "" {
		subject, commitBody, err := s.Git.LastCommit(ctx, dir, rev)
		if err == nil {
			if title == "" {
				title = strings.TrimSpace(subject)
			}
			if body == "" {
				body = strings.TrimSpace(commitBody)
			}
		}
	}
	if title == "" {
		return "", "", clierr.Failed("PR title is required (specify with --title)")
	}
	return title, body, nil
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) icon(glyph string) string {
	if glyph == "" {
		return ""
	}
	return glyph + " "
}

func (s *Service) bold(text string) string {
	if s.UI.Bold == nil {
		return text
	}
	return s.UI.Bold(text)
}

func (s *Service) title(text string) string {
	if s.UI.Title == nil {
		return text
	}
	return s.UI.Title(text)
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Service) sleep(d time.Duration) {
	if s.Sleep == nil {
		time.Sleep(d)
		return
	}
	s.Sleep(d)
}

// withMessage prefixes msg while keeping the error's clierr code when it has
// one.
func withMessage(err error, msg string) error {
	var c *clierr.Error
	if errors.As(err, &c) {
		return &clierr.Error{Code: c.Code, Exit: c.Exit, Message: msg, Hint: c.Hint, Err: err}
	}
	return clierr.Wrap(err, clierr.Failed(msg))
}
