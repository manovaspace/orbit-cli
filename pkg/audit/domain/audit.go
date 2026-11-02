// Package domain holds the pure audit types and rules: findings, severities,
// reports and the parsers for the manifests the audit reads. It performs no
// I/O, runs no external programs and imports no adapter.
package domain

import (
	"sort"
	"strings"
)

// SchemaVersion is the audit report schema version carried in JSON output.
const SchemaVersion = 1

// Severity is the weight of one finding.
type Severity string

const (
	// SeverityError fails the command with exit code 1.
	SeverityError Severity = "ERROR"
	// SeverityWarn reports drift or risk without failing the command.
	SeverityWarn Severity = "WARN"
	// SeverityInfo reports an accepted exception or informational detail.
	SeverityInfo Severity = "INFO"
)

// Finding is one audit problem, or one accepted exception.
type Finding struct {
	ID       string   `json:"id" yaml:"id"`
	Severity Severity `json:"severity" yaml:"severity"`
	Subject  string   `json:"subject" yaml:"subject"`
	Expected string   `json:"expected,omitempty" yaml:"expected,omitempty"`
	Actual   string   `json:"actual,omitempty" yaml:"actual,omitempty"`
	Fix      string   `json:"fix,omitempty" yaml:"fix,omitempty"`
}

// Report is one audit run.
type Report struct {
	SchemaVersion int       `json:"schemaVersion" yaml:"schemaVersion"`
	Findings      []Finding `json:"findings" yaml:"findings"`
	Unverified    []string  `json:"unverified" yaml:"unverified"`
}

// NewReport creates an empty report stamped with the schema version.
func NewReport() Report {
	return Report{SchemaVersion: SchemaVersion, Findings: []Finding{}, Unverified: []string{}}
}

// Add appends one finding.
func (r *Report) Add(f Finding) { r.Findings = append(r.Findings, f) }

// AddAll appends findings.
func (r *Report) AddAll(fs []Finding) { r.Findings = append(r.Findings, fs...) }

// AddUnverified records something the audit could not check. Repeated messages
// are kept once so a merged run stays readable.
func (r *Report) AddUnverified(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	for _, existing := range r.Unverified {
		if existing == message {
			return
		}
	}
	r.Unverified = append(r.Unverified, message)
}

// HasErrors reports whether any finding is an ERROR.
func (r Report) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Counts returns the number of ERROR and WARN findings.
func (r Report) Counts() (errors, warnings int) {
	for _, f := range r.Findings {
		switch f.Severity {
		case SeverityError:
			errors++
		case SeverityWarn:
			warnings++
		}
	}
	return errors, warnings
}

// ExitCode is 1 with an ERROR, 2 when something could not be verified and
// nothing failed, and 0 otherwise. WARN and INFO findings exit 0.
func (r Report) ExitCode() int {
	if r.HasErrors() {
		return 1
	}
	if len(r.Unverified) > 0 {
		return 2
	}
	return 0
}

// Merge appends other into r, keeping clone warnings and unverified messages
// once when several checks scan the same repositories.
func (r *Report) Merge(other Report) {
	seen := map[string]bool{}
	for _, f := range r.Findings {
		if f.ID == "clone.behind" {
			seen[f.Subject] = true
		}
	}
	for _, f := range other.Findings {
		if f.ID == "clone.behind" && seen[f.Subject] {
			continue
		}
		r.Add(f)
	}
	for _, u := range other.Unverified {
		r.AddUnverified(u)
	}
}

// SkipPath reports repository paths the audit must ignore: vendored trees,
// build output, worktrees and the temporary -wt- worktree copies.
func SkipPath(path string) bool {
	slashed := "/" + strings.Trim(filepathSlash(path), "/") + "/"
	for _, skip := range []string{"/node_modules/", "/.next/", "/.worktrees/"} {
		if strings.Contains(slashed, skip) {
			return true
		}
	}
	return strings.Contains(slashed, "-wt-")
}

// filepathSlash normalizes separators without importing path/filepath, whose
// behaviour the domain rules must not depend on.
func filepathSlash(path string) string { return strings.ReplaceAll(path, "\\", "/") }

// Versions returns the distinct versions sorted ascending.
func Versions(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i], out[j]) < 0 })
	return out
}
