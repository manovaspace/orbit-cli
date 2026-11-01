// Package ghcli implements the pull-request forge port with the GitHub CLI.
package ghcli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Forge calls `gh` through a runner.Runner.
type Forge struct {
	Runner runner.Runner
}

// New builds a Forge over r.
func New(r runner.Runner) *Forge {
	return &Forge{Runner: r}
}

func (f *Forge) run(ctx context.Context, args ...string) (runner.Result, error) {
	return f.Runner.Run(ctx, "", "gh", args...)
}

// CreatePR opens a pull request and parses its number from the URL.
func (f *Forge) CreatePR(ctx context.Context, repo domain.Repo, title, body, head, base string) (domain.PullRequest, error) {
	res, err := f.run(ctx, "pr", "create", "-R", repo.String(), "--title", title, "--base", base, "--head", head, "--body", body)
	if err != nil {
		return domain.PullRequest{}, ghError(err, res)
	}

	url := strings.TrimSpace(res.Stdout)
	created := domain.PullRequest{URL: url, Title: title, Head: head, Base: base}
	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		if number, parseErr := strconv.ParseInt(url[idx+1:], 10, 64); parseErr == nil {
			created.Number = number
		}
	}
	return created, nil
}

// MergePR merges a pull request with the given method.
func (f *Forge) MergePR(ctx context.Context, repo domain.Repo, ref string, method domain.Method) error {
	args := []string{"pr", "merge", ref, "-R", repo.String()}
	switch method {
	case domain.MethodSquash:
		args = append(args, "--squash")
	case domain.MethodRebase:
		args = append(args, "--rebase")
	default:
		args = append(args, "--merge")
	}
	res, err := f.run(ctx, args...)
	if err != nil {
		return ghError(err, res)
	}
	return nil
}

// Checks reads the pull request's checks. A nonzero gh exit is not a failure
// when stdout parses as a JSON array: gh exits nonzero for failing and pending
// checks too. The text "no checks reported" maps to an empty slice.
func (f *Forge) Checks(ctx context.Context, repo domain.Repo, ref string) ([]domain.Check, error) {
	res, err := f.run(ctx, "pr", "checks", ref, "-R", repo.String(), "--json", "name,bucket")

	checks, parseErr := domain.ParseChecks([]byte(strings.TrimSpace(res.Stdout)))
	if parseErr == nil {
		return checks, nil
	}

	if strings.Contains(res.Stderr, "no checks reported") || strings.Contains(res.Stdout, "no checks reported") {
		return []domain.Check{}, nil
	}

	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.Stdout)
	}
	if detail == "" && err != nil {
		detail = err.Error()
	}
	if detail == "" {
		detail = parseErr.Error()
	}
	message := "reading checks: " + detail
	if isNetworkFailure(detail) {
		return nil, clierr.Network(message)
	}
	return nil, clierr.Tool(message)
}

// Comment posts a comment on the pull request.
func (f *Forge) Comment(ctx context.Context, repo domain.Repo, ref, body string) error {
	res, err := f.run(ctx, "pr", "comment", ref, "-R", repo.String(), "--body", body)
	if err != nil {
		return ghError(err, res)
	}
	return nil
}

// ListOpen lists the repository's open pull requests.
func (f *Forge) ListOpen(ctx context.Context, repo domain.Repo) ([]domain.PullRequest, error) {
	res, err := f.run(ctx, "pr", "list", "-R", repo.String(), "--state", "open", "--json", "number,title,headRefName,baseRefName,author,url")
	if err != nil {
		return nil, ghError(err, res)
	}

	var rows []struct {
		Number      int64  `json:"number"`
		Title       string `json:"title"`
		HeadRefName string `json:"headRefName"`
		BaseRefName string `json:"baseRefName"`
		Author      struct {
			Login string `json:"login"`
		} `json:"author"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &rows); err != nil {
		return nil, clierr.Tool("parsing GitHub PR list: " + err.Error())
	}

	open := make([]domain.PullRequest, 0, len(rows))
	for _, row := range rows {
		open = append(open, domain.PullRequest{
			Number: row.Number,
			URL:    row.URL,
			Title:  row.Title,
			Head:   row.HeadRefName,
			Base:   row.BaseRefName,
			Author: row.Author.Login,
		})
	}
	return open, nil
}

// ghError keeps the runner's clierr code and prefers gh's own stderr text as
// the message.
func ghError(err error, res runner.Result) error {
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.Stdout)
	}
	if detail == "" {
		return err
	}
	return &clierr.Error{Code: clierr.Code(err), Exit: clierr.ExitCode(err), Message: detail}
}

// networkMarkers are the transport failures gh reports when it cannot reach
// GitHub; everything else is treated as an external-tool failure.
var networkMarkers = []string{
	"dial tcp",
	"no such host",
	"could not resolve host",
	"connection refused",
	"connection reset",
	"network is unreachable",
	"i/o timeout",
	"tls handshake",
	"http 5",
	"timeout awaiting response",
}

func isNetworkFailure(detail string) bool {
	lower := strings.ToLower(detail)
	for _, marker := range networkMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
