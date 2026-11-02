// Package domain holds the release and demo rules that need no I/O.
package domain

import (
	"fmt"
	"net/url"
	"strings"

	prdomain "github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

// Release heads ADR-044 recognises. There is no long-lived release branch.
const (
	// ChangesetsHead is the head branch changesets/action opens.
	ChangesetsHead = "changeset-release/main"
	// ReleasePleasePrefix is the head prefix release-please opens.
	ReleasePleasePrefix = "release-please--"
	// MainBranch is the only branch a demo may promote from and the ref a
	// library publish runs on.
	MainBranch = "main"
	// DemoBranch is the only branch that deploys a demo.
	DemoBranch = "demo"
	// PublishWorkflow dispatches a library publish.
	PublishWorkflow = "publish.yml"
	// NpmPublishEnvironment gates publishing of @manovaspace/ui and
	// @manovaspace/tokens. The owner approves it; the CLI never does.
	NpmPublishEnvironment = "npm-publish"
	// ChangesetConfig is the file that marks a library repository.
	ChangesetConfig = ".changeset/config.json"
)

// ReleaseSetupFiles are the files that declare an automatic release setup.
// A repository with none of them has nothing for `release status` to report
// when no repository is named explicitly.
var ReleaseSetupFiles = []string{
	ChangesetConfig,
	"release-please-config.json",
	".release-please-manifest.json",
	".github/workflows/publish.yml",
	".github/workflows/publish-image.yml",
	".github/workflows/release.yml",
	".github/workflows/deploy.yml",
}

// IsReleaseHead reports whether head is a bot release pull-request branch.
func IsReleaseHead(head string) bool {
	head = strings.TrimSpace(head)
	if head == ChangesetsHead {
		return true
	}
	return strings.HasPrefix(head, ReleasePleasePrefix)
}

// IsLibrary reports whether the repository uses Changesets and therefore
// publishes npm packages through the owner-gated publish workflow.
func IsLibrary(changesetConfigExists bool) bool {
	return changesetConfigExists
}

// IsPublishOrDeployWorkflow reports whether a workflow run name is a publish
// or deploy run. CI, CodeQL and release build runs are not reported.
func IsPublishOrDeployWorkflow(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(lower, "publish") || strings.Contains(lower, "deploy")
}

// CheckState summarises a check list as pass, fail, pending, none or unknown.
func CheckState(checks []prdomain.Check) string {
	if len(checks) == 0 {
		return "none"
	}
	pending := false
	unknown := false
	for _, check := range checks {
		switch check.Bucket {
		case "fail", "cancel":
			return "fail"
		case "pending":
			pending = true
		case "pass", "skipping":
		default:
			unknown = true
		}
	}
	if pending {
		return "pending"
	}
	if unknown {
		return "unknown"
	}
	return "pass"
}

// DatabaseNamePrefix is the prefix of every throwaway database.
const DatabaseNamePrefix = "test_"

// ValidDatabaseName reports whether name is a throwaway database name this
// command created. It is the only input that reaches a CREATE/DROP statement.
func ValidDatabaseName(name string) bool {
	if !strings.HasPrefix(name, DatabaseNamePrefix) || len(name) <= len(DatabaseNamePrefix) {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

// ReplaceDatabaseName returns rawURL with its database path replaced by
// database. It never returns the URL in an error, because the URL carries the
// dev password.
func ReplaceDatabaseName(rawURL, database string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("dev database URL is not a valid URL")
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("dev database URL has no scheme or host")
	}
	parsed.Path = "/" + database
	return parsed.String(), nil
}

// Redact replaces every secret value in text with a placeholder. It is used on
// command output so a URL or password never reaches the terminal.
func Redact(text string, secrets []string) string {
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) < 8 {
			continue
		}
		text = strings.ReplaceAll(text, secret, "<redacted>")
	}
	return text
}
