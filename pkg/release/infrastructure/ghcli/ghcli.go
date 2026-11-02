// Package ghcli implements the release forge port with the GitHub CLI.
package ghcli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	prdomain "github.com/manovaspace/orbit-cli/pkg/pr/domain"
	"github.com/manovaspace/orbit-cli/pkg/release/application"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Forge calls `gh` through a runner.Runner.
type Forge struct {
	Runner runner.Runner
}

// New builds a Forge over r.
func New(r runner.Runner) *Forge { return &Forge{Runner: r} }

func (f *Forge) run(ctx context.Context, args ...string) (runner.Result, error) {
	return f.Runner.Run(ctx, "", "gh", args...)
}

// ListOpen lists the repository's open pull requests.
func (f *Forge) ListOpen(ctx context.Context, slug string) ([]application.PullRequest, error) {
	res, err := f.run(ctx, "pr", "list", "-R", slug, "--state", "open", "--json", "number,title,headRefName,baseRefName,author,url")
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
	open := make([]application.PullRequest, 0, len(rows))
	for _, row := range rows {
		open = append(open, application.PullRequest{
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

// Checks reads a pull request's checks. A nonzero gh exit is not a failure
// when stdout parses as a JSON array, because gh exits nonzero for failing and
// pending checks too.
func (f *Forge) Checks(ctx context.Context, slug, ref string) ([]prdomain.Check, error) {
	res, err := f.run(ctx, "pr", "checks", ref, "-R", slug, "--json", "name,bucket")
	checks, parseErr := prdomain.ParseChecks([]byte(strings.TrimSpace(res.Stdout)))
	if parseErr == nil {
		return checks, nil
	}
	if strings.Contains(res.Stderr, "no checks reported") || strings.Contains(res.Stdout, "no checks reported") {
		return []prdomain.Check{}, nil
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

// CommitChecks reads the check runs of one commit through the REST API. A
// commit has no `gh pr checks`; the demo gate reads the merged main commit.
func (f *Forge) CommitChecks(ctx context.Context, slug, sha string) ([]prdomain.Check, error) {
	res, err := f.run(ctx, "api", "repos/"+slug+"/commits/"+sha+"/check-runs", "--paginate", "--jq", ".check_runs[]")
	if err != nil && strings.TrimSpace(res.Stdout) == "" {
		return nil, ghError(err, res)
	}
	var checks []prdomain.Check
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, clierr.Tool("parsing commit checks: " + err.Error())
		}
		checks = append(checks, prdomain.Check{Name: row.Name, Bucket: checkBucket(row.Status, row.Conclusion)})
	}
	return checks, nil
}

// MergePR merges a pull request with the given method.
func (f *Forge) MergePR(ctx context.Context, slug, ref string, method prdomain.Method) error {
	args := []string{"pr", "merge", ref, "-R", slug}
	switch method {
	case prdomain.MethodSquash:
		args = append(args, "--squash")
	case prdomain.MethodRebase:
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

// Comment posts a comment on the pull request.
func (f *Forge) Comment(ctx context.Context, slug, ref, body string) error {
	res, err := f.run(ctx, "pr", "comment", ref, "-R", slug, "--body", body)
	if err != nil {
		return ghError(err, res)
	}
	return nil
}

// BranchSHA reads a branch head. A 404 is a missing branch, not a failure.
func (f *Forge) BranchSHA(ctx context.Context, slug, branch string) (string, bool, error) {
	res, err := f.run(ctx, "api", "repos/"+slug+"/git/ref/heads/"+branch, "--jq", ".object.sha")
	if err != nil {
		if isNotFound(res) {
			return "", false, nil
		}
		return "", false, ghError(err, res)
	}
	sha := strings.TrimSpace(res.Stdout)
	if sha == "" {
		return "", false, nil
	}
	return sha, true, nil
}

// CreateBranch creates a branch at sha through the REST API.
func (f *Forge) CreateBranch(ctx context.Context, slug, branch, sha string) error {
	res, err := f.run(ctx, "api", "-X", "POST", "repos/"+slug+"/git/refs", "-f", "ref=refs/heads/"+branch, "-f", "sha="+sha)
	if err != nil {
		return ghError(err, res)
	}
	return nil
}

// CreatePR opens a pull request and parses its number from the URL.
func (f *Forge) CreatePR(ctx context.Context, slug, title, body, head, base string) (application.PullRequest, error) {
	res, err := f.run(ctx, "pr", "create", "-R", slug, "--title", title, "--base", base, "--head", head, "--body", body)
	if err != nil {
		return application.PullRequest{}, ghError(err, res)
	}
	url := strings.TrimSpace(res.Stdout)
	created := application.PullRequest{URL: url, Title: title, Head: head, Base: base}
	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		if number, parseErr := strconv.ParseInt(url[idx+1:], 10, 64); parseErr == nil {
			created.Number = number
		}
	}
	return created, nil
}

// Runs lists recent workflow runs.
func (f *Forge) Runs(ctx context.Context, slug string) ([]application.WorkflowRun, error) {
	res, err := f.run(ctx, "run", "list", "-R", slug, "--limit", "20", "--json", "name,displayTitle,status,conclusion,headBranch,createdAt,url")
	if err != nil {
		return nil, ghError(err, res)
	}
	var rows []application.WorkflowRun
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &rows); err != nil {
		return nil, clierr.Tool("parsing workflow runs: " + err.Error())
	}
	return rows, nil
}

// Dispatch runs a workflow on ref.
func (f *Forge) Dispatch(ctx context.Context, slug, workflow, ref string) error {
	res, err := f.run(ctx, "workflow", "run", workflow, "-R", slug, "--ref", ref)
	if err != nil {
		return ghError(err, res)
	}
	return nil
}

// CompareAhead returns how many commits head has that base lacks.
func (f *Forge) CompareAhead(ctx context.Context, slug, base, head string) (int, error) {
	res, err := f.run(ctx, "api", "repos/"+slug+"/compare/"+base+"..."+head, "--jq", ".ahead_by")
	if err != nil {
		return 0, ghError(err, res)
	}
	ahead, parseErr := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if parseErr != nil {
		return 0, clierr.Tool("parsing compare result: " + parseErr.Error())
	}
	return ahead, nil
}

func checkBucket(status, conclusion string) string {
	switch conclusion {
	case "success":
		return "pass"
	case "failure", "timed_out", "action_required", "stale":
		return "fail"
	case "cancelled":
		return "cancel"
	case "skipped", "neutral":
		return "skipping"
	}
	switch status {
	case "queued", "in_progress", "requested", "waiting", "pending":
		return "pending"
	}
	return "pending"
}

func isNotFound(res runner.Result) bool {
	detail := strings.ToLower(res.Stderr + "\n" + res.Stdout)
	return strings.Contains(detail, "not found") || strings.Contains(detail, "http 404") || strings.Contains(detail, "404")
}

// ghError keeps the runner's clierr code and prefers gh's own stderr text.
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
