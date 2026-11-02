package domain

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// StaticOnlyRepos are repositories whose deliverable is static content: they
// have no test suite by design and `o audit tests` never reports them.
var StaticOnlyRepos = []string{
	"manovaspace/docs",
	"clients/kohan_kherad/website",
	"clients/jtash/frontend",
	"clients/manova/manova-frontend",
	"clients/meow/pishidolfini",
	"handbook",
}

// StaticOnlyRepo reports whether path is one of the static-only sites.
func StaticOnlyRepo(path string) bool {
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	for _, candidate := range StaticOnlyRepos {
		if path == candidate {
			return true
		}
	}
	return false
}

// IsTestFile reports whether path names a test file. The caller is
// responsible for not walking vendored or generated trees.
func IsTestFile(path string) bool {
	slashed := strings.ReplaceAll(path, "\\", "/")
	base := slashed
	if idx := strings.LastIndexAny(slashed, "/\\"); idx >= 0 {
		base = slashed[idx+1:]
	}
	if base == "" {
		return false
	}
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	for _, suffix := range []string{
		".test.ts", ".test.tsx", ".test.js", ".test.jsx", ".test.mjs", ".test.cjs",
		".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx", ".spec.mjs", ".spec.cjs",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	if strings.HasSuffix(base, "_test.py") {
		return true
	}
	if strings.HasSuffix(base, ".sh") {
		if strings.HasPrefix(base, "test-") || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.sh") || strings.HasSuffix(base, ".test.sh") {
			return true
		}
	}
	return false
}

// GitHubSlug reads owner/repository from a GitHub remote URL. Non-GitHub
// remotes and malformed values return ok false.
func GitHubSlug(remote string) (string, bool) {
	value := strings.TrimSpace(remote)
	if value == "" {
		return "", false
	}
	const marker = "github.com"
	idx := strings.Index(value, marker)
	if idx < 0 {
		return "", false
	}
	rest := strings.TrimPrefix(value[idx+len(marker):], ":")
	rest = strings.TrimPrefix(rest, "/")
	rest = strings.TrimSuffix(rest, ".git")
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// MergedCheck is one check of a merged pull request, normalised to the same
// buckets the merge gate uses.
type MergedCheck struct {
	Name   string
	Bucket string // pass, fail, pending, skipping
}

// MergedPullRequest is one merged pull request read from GitHub.
type MergedPullRequest struct {
	Number   int
	Title    string
	URL      string
	MergedAt time.Time
	Comments []string
	Checks   []MergedCheck
}

// HasOverride reports whether any comment starts with "override:".
func (p MergedPullRequest) HasOverride() bool {
	for _, comment := range p.Comments {
		if strings.HasPrefix(strings.TrimSpace(comment), "override:") {
			return true
		}
	}
	return false
}

// NoChecks reports whether GitHub reported no checks at all.
func (p MergedPullRequest) NoChecks() bool { return len(p.Checks) == 0 }

// FailingChecks returns the names of checks that did not pass. Pending and
// unknown states count as failing: the pull request merged before they
// settled.
func (p MergedPullRequest) FailingChecks() []string {
	var names []string
	for _, check := range p.Checks {
		switch check.Bucket {
		case "pass", "skipping":
		default:
			names = append(names, check.Name)
		}
	}
	sort.Strings(names)
	return names
}

// MergedSearch is the GitHub search qualifier for pull requests merged since
// the given day.
func MergedSearch(since time.Time) string {
	return "merged:>=" + since.UTC().Format("2006-01-02")
}

// ActionsUsage is one owner's GitHub Actions billing counter.
type ActionsUsage struct {
	TotalMinutesUsed int
	IncludedMinutes  int
}

// PackageRef is one resolved dependency version queried against OSV.
type PackageRef struct {
	Name      string
	Version   string
	Ecosystem string // npm or Go
}

// Advisory is one OSV advisory.
type Advisory struct {
	ID      string
	Summary string
}

// AdvisoryHit ties one advisory to the resolved package version that carries
// it.
type AdvisoryHit struct {
	Package    PackageRef
	Advisories []Advisory
}

var evalSummary = regexp.MustCompile(`recall@5=([0-9.]+)\s+mrr=([0-9.]+)`)

// ParseEvalSummary reads the recall@5 and MRR numbers printed by
// handbook-mcp/eval.mjs.
func ParseEvalSummary(output string) (recall, mrr string, ok bool) {
	match := evalSummary.FindStringSubmatch(output)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}
