package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Check is a single status check reported by `gh pr checks --json name,bucket`.
type Check struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"` // pass, fail, pending, skipping, cancel
}

// GateResult is the outcome of evaluating a pull request's checks.
type GateResult struct {
	Allowed bool
	Reason  string
}

// ciHeadPrefixes are the head branch prefixes of pull requests that must pass
// a real CI check before they merge: release pull requests from
// release-please and Changesets, and Renovate dependency updates.
var ciHeadPrefixes = []string{"release-please--", "changeset-release/", "renovate/"}

// secretScanPrefix is the check name prefix of the secret scan. It runs on
// every push, so it says nothing about whether the change was tested.
const secretScanPrefix = "secret-scan"

// ParseChecks decodes the JSON array emitted by `gh pr checks --json name,bucket`.
func ParseChecks(data []byte) ([]Check, error) {
	var checks []Check
	if err := json.Unmarshal(data, &checks); err != nil {
		return nil, fmt.Errorf("parsing checks: %w", err)
	}
	return checks, nil
}

// RequiresCI reports whether a pull request from headRef must have a passing
// CI check before it merges. Private repositories run CI only on release and
// Renovate pull requests, so only those need one.
func RequiresCI(headRef string) bool {
	for _, prefix := range ciHeadPrefixes {
		if strings.HasPrefix(headRef, prefix) {
			return true
		}
	}
	return false
}

// IsSecretScan reports whether a check is the secret scan, which runs on every
// push and does not count as CI.
func IsSecretScan(name string) bool {
	return strings.HasPrefix(name, secretScanPrefix)
}

// EvaluateChecks decides whether a pull request may merge. Failed or cancelled
// checks are refused first, then pending checks, then unknown check states.
// With requireCI, an empty list is refused and at least one passing check that
// is not the secret scan is required. Without requireCI, an empty list, an
// all-skipped list and a secret-scan-only list are allowed.
func EvaluateChecks(checks []Check, requireCI bool) GateResult {
	if len(checks) == 0 {
		if requireCI {
			return GateResult{Reason: "no checks reported"}
		}
		return GateResult{Allowed: true}
	}

	var failed []string
	var pending []string
	var unknown *Check
	passedCI := false

	for i := range checks {
		c := checks[i]
		switch c.Bucket {
		case "fail", "cancel":
			failed = append(failed, c.Name)
		case "pending":
			pending = append(pending, c.Name)
		case "pass":
			if !IsSecretScan(c.Name) {
				passedCI = true
			}
		case "skipping":
			// Neutral outcome: nothing to report.
		default:
			if unknown == nil {
				unknown = &checks[i]
			}
		}
	}

	if len(failed) > 0 {
		return GateResult{Reason: "checks failed: " + strings.Join(failed, ", ")}
	}
	if len(pending) > 0 {
		return GateResult{Reason: "checks still running: " + strings.Join(pending, ", ")}
	}
	if unknown != nil {
		return GateResult{Reason: fmt.Sprintf("unknown check state %q for %s", unknown.Bucket, unknown.Name)}
	}
	if requireCI && !passedCI {
		return GateResult{Reason: "no CI check passed (release and Renovate pull requests need CI)"}
	}

	return GateResult{Allowed: true}
}
