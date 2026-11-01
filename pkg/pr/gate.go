package pr

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

// ParseChecks decodes the JSON array emitted by `gh pr checks --json name,bucket`.
func ParseChecks(data []byte) ([]Check, error) {
	var checks []Check
	if err := json.Unmarshal(data, &checks); err != nil {
		return nil, fmt.Errorf("parsing checks: %w", err)
	}
	return checks, nil
}

// EvaluateChecks decides whether a pull request may merge. An empty list is
// refused first, then failed or cancelled checks, then pending checks, then
// unknown check states.
func EvaluateChecks(checks []Check) GateResult {
	if len(checks) == 0 {
		return GateResult{Reason: "no checks reported"}
	}

	var failed []string
	var pending []string
	var unknown *Check

	for i := range checks {
		c := checks[i]
		switch c.Bucket {
		case "fail", "cancel":
			failed = append(failed, c.Name)
		case "pending":
			pending = append(pending, c.Name)
		case "pass", "skipping":
			// Neutral outcomes: nothing to report.
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

	return GateResult{Allowed: true}
}
