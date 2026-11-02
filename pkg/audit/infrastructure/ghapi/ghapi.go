// Package ghapi implements the audit GitHub port with the GitHub CLI. It runs
// only read-only gh commands.
package ghapi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Client calls gh through a runner.Runner.
type Client struct {
	Runner runner.Runner
}

// New builds a Client over r.
func New(r runner.Runner) *Client { return &Client{Runner: r} }

func (c *Client) run(ctx context.Context, args ...string) (runner.Result, error) {
	return c.Runner.Run(ctx, "", "gh", args...)
}

// Releases returns the latest release tag of owner/repo. Repositories that
// publish only tags, such as golang/go, fall back to the newest tag.
func (c *Client) Releases(ctx context.Context, owner, repo string) (string, error) {
	res, err := c.run(ctx, "api", "repos/"+owner+"/"+repo+"/releases/latest", "--jq", ".tag_name")
	if err != nil && isNotFound(res) {
		res, err = c.run(ctx, "api", "repos/"+owner+"/"+repo+"/tags", "--paginate", "--jq", ".[].name")
		if err == nil {
			if tag := newestTag(res.Stdout); tag != "" {
				return tag, nil
			}
		}
	}
	if err != nil {
		return "", ghError(err, res)
	}
	tag := strings.TrimSpace(res.Stdout)
	if tag == "" {
		return "", clierr.Tool("releases: " + owner + "/" + repo + " reported no tag")
	}
	return tag, nil
}

// newestTag picks the highest version tag from one tag per line.
func newestTag(output string) string {
	best := ""
	for _, line := range strings.Split(output, "\n") {
		tag := strings.TrimSpace(line)
		if tag == "" {
			continue
		}
		trimmed := strings.TrimPrefix(strings.TrimPrefix(tag, "go"), "v")
		if trimmed == "" || trimmed[0] < '0' || trimmed[0] > '9' {
			continue
		}
		if best == "" || domain.CompareVersions(tag, best) > 0 {
			best = tag
		}
	}
	return best
}

// MergedPullRequests lists pull requests merged since the given day.
func (c *Client) MergedPullRequests(ctx context.Context, slug string, since time.Time) ([]domain.MergedPullRequest, error) {
	res, err := c.run(ctx,
		"pr", "list", "-R", slug,
		"--state", "merged",
		"--search", domain.MergedSearch(since),
		"--limit", "200",
		"--json", "number,title,url,mergedAt,comments,statusCheckRollup",
	)
	if err != nil {
		return nil, ghError(err, res)
	}
	var rows []struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		MergedAt string `json:"mergedAt"`
		Comments []struct {
			Body string `json:"body"`
		} `json:"comments"`
		StatusCheckRollup []struct {
			Typename   string `json:"__typename"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			Context    string `json:"context"`
			State      string `json:"state"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &rows); err != nil {
		return nil, clierr.Tool("parsing merged pull requests: " + err.Error())
	}
	out := make([]domain.MergedPullRequest, 0, len(rows))
	for _, row := range rows {
		mergedAt, _ := time.Parse(time.RFC3339, row.MergedAt)
		if !mergedAt.IsZero() && mergedAt.Before(since) {
			continue
		}
		pr := domain.MergedPullRequest{Number: row.Number, Title: row.Title, URL: row.URL, MergedAt: mergedAt}
		for _, comment := range row.Comments {
			pr.Comments = append(pr.Comments, comment.Body)
		}
		for _, check := range row.StatusCheckRollup {
			name := check.Name
			if name == "" {
				name = check.Context
			}
			pr.Checks = append(pr.Checks, domain.MergedCheck{Name: name, Bucket: rollupBucket(check.Status, check.Conclusion, check.State)})
		}
		out = append(out, pr)
	}
	return out, nil
}

// ActionsUsage reads an owner's Actions minutes from the consolidated billing
// usage report. A personal account is read through the user endpoint when the
// organization endpoint is 404. The report carries no included-minutes
// counter, so IncludedMinutes stays zero.
func (c *Client) ActionsUsage(ctx context.Context, owner string) (domain.ActionsUsage, error) {
	res, err := c.run(ctx, "api", "organizations/"+owner+"/settings/billing/usage")
	if err != nil && isNotFound(res) {
		res, err = c.run(ctx, "api", "users/"+owner+"/settings/billing/usage")
	}
	if err != nil {
		return domain.ActionsUsage{}, ghError(err, res)
	}
	var body struct {
		UsageItems []struct {
			Product  string  `json:"product"`
			UnitType string  `json:"unitType"`
			Quantity float64 `json:"quantity"`
		} `json:"usageItems"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &body); err != nil {
		return domain.ActionsUsage{}, clierr.Tool("parsing Actions usage: " + err.Error())
	}
	minutes := 0.0
	for _, item := range body.UsageItems {
		if !strings.EqualFold(item.Product, "actions") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(item.UnitType), "minute") {
			continue
		}
		minutes += item.Quantity
	}
	return domain.ActionsUsage{TotalMinutesUsed: int(minutes + 0.5)}, nil
}

func rollupBucket(status, conclusion, state string) string {
	if state != "" {
		switch strings.ToUpper(state) {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			return "pass"
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STALE":
			return "fail"
		default:
			return "pending"
		}
	}
	switch strings.ToUpper(conclusion) {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return "pass"
	case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STALE", "STARTUP_FAILURE":
		return "fail"
	}
	if conclusion == "" {
		return "pending"
	}
	switch strings.ToUpper(status) {
	case "COMPLETED":
		return "pending"
	}
	return "pending"
}

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

func isNotFound(res runner.Result) bool {
	detail := strings.ToLower(res.Stderr + "\n" + res.Stdout)
	return strings.Contains(detail, "not found") || strings.Contains(detail, "http 404") || strings.Contains(detail, " 404")
}
