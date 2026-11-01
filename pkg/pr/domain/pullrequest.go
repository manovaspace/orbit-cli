package domain

import (
	"fmt"
	"strings"
	"time"
)

// Method is a pull-request merge method.
type Method string

// Supported merge methods.
const (
	MethodMerge  Method = "merge"
	MethodSquash Method = "squash"
	MethodRebase Method = "rebase"
)

// ParseMethod validates a --method value. Blank means merge, and the legacy
// "rebase-merge" spelling is accepted as rebase.
func ParseMethod(value string) (Method, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(MethodMerge):
		return MethodMerge, nil
	case string(MethodSquash):
		return MethodSquash, nil
	case string(MethodRebase), "rebase-merge":
		return MethodRebase, nil
	}
	return "", fmt.Errorf("invalid merge method %q: must be one of 'merge', 'squash', 'rebase', 'rebase-merge'", value)
}

// PullRequest identifies a pull request on a forge.
type PullRequest struct {
	Number int64  `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Head   string `json:"head"`
	Base   string `json:"base"`
	Author string `json:"author"`
}

// MergeOptions are the merge-gate options shared by merge, create --merge and
// deliver.
type MergeOptions struct {
	Override      string
	OverrideSet   bool
	AllowNoChecks bool
	Wait          bool
	WaitTimeout   time.Duration
}
