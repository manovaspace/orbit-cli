// Package domain holds the workspace types and rules that need no I/O.
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Stable InspectRepo / status Error values. The CLI must not collapse these.
const (
	ErrMissing = "missing"
	ErrGitless = "not a git repository"
)

// Reasons a repository is skipped instead of updated.
const (
	SkipUncommitted    = "working tree has uncommitted changes"
	SkipNotFastForward = "branch cannot be fast-forwarded"
	NotCloned          = "repository not cloned"
)

// RepoStatus represents the git status of a repository in the workspace.
type RepoStatus struct {
	Name          string `json:"name" yaml:"name"`
	Path          string `json:"path" yaml:"path"`
	CurrentBranch string `json:"current_branch" yaml:"current_branch"`
	IsClean       bool   `json:"is_clean" yaml:"is_clean"`
	AheadCount    int    `json:"ahead_count" yaml:"ahead_count"`
	BehindCount   int    `json:"behind_count" yaml:"behind_count"`
	ModifiedCount int    `json:"modified_count" yaml:"modified_count"`
	Error         string `json:"error,omitempty" yaml:"error,omitempty"`
}

// CloneResult captures the outcome of cloning a repository target.
type CloneResult struct {
	Name          string `json:"name" yaml:"name"`
	Path          string `json:"path" yaml:"path"`
	Success       bool   `json:"success" yaml:"success"`
	AlreadyExists bool   `json:"already_exists" yaml:"already_exists"`
	Error         string `json:"error,omitempty" yaml:"error,omitempty"`
}

// SyncResult captures the outcome of synchronizing a repository target with remote.
type SyncResult struct {
	Name          string `json:"name" yaml:"name"`
	Path          string `json:"path" yaml:"path"`
	Success       bool   `json:"success" yaml:"success"`
	FastForwarded bool   `json:"fast_forwarded" yaml:"fast_forwarded"`
	SkippedReason string `json:"skipped_reason,omitempty" yaml:"skipped_reason,omitempty"`
	Error         string `json:"error,omitempty" yaml:"error,omitempty"`
	AssetError    string `json:"asset_error,omitempty" yaml:"asset_error,omitempty"`
}

// RepairResult captures the outcome of attaching .git to a gitless tree.
type RepairResult struct {
	Name    string `json:"name" yaml:"name"`
	Path    string `json:"path" yaml:"path"`
	Success bool   `json:"success" yaml:"success"`
	Skipped string `json:"skipped,omitempty" yaml:"skipped,omitempty"`
	Error   string `json:"error,omitempty" yaml:"error,omitempty"`
}

// ModifiedCount counts the non-empty lines of `git status --porcelain` output.
func ModifiedCount(porcelain string) int {
	trimmed := strings.TrimSpace(porcelain)
	if trimmed == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(trimmed, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// ParseAheadBehind reads the two counts of `git rev-list --left-right --count`.
// Malformed output yields zeros, matching the old "no upstream means no
// counts" behaviour.
func ParseAheadBehind(output string) (ahead, behind int) {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 2 {
		return 0, 0
	}
	if a, err := strconv.Atoi(fields[0]); err == nil {
		ahead = a
	}
	if b, err := strconv.Atoi(fields[1]); err == nil {
		behind = b
	}
	return ahead, behind
}

// IsFastForward reports whether `git merge --ff-only` actually moved HEAD.
func IsFastForward(mergeOutput string) bool {
	return strings.Contains(mergeOutput, "Fast-forward") || strings.Contains(mergeOutput, "Updating")
}

// NotDefaultBranch is the skip reason used when the checkout is not on the
// default branch.
func NotDefaultBranch(current, defaultBranch string) string {
	return fmt.Sprintf("current branch '%s' is not default branch '%s'", current, defaultBranch)
}
