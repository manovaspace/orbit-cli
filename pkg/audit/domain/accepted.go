package domain

import (
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AcceptedEntry is one named UNVERIFIED item the workspace accepts for now.
// The real file is handbook/agents/workspace/audit-accepted.yaml.
type AcceptedEntry struct {
	Check  string `yaml:"check" json:"check"`
	Match  string `yaml:"match" json:"match"`
	Reason string `yaml:"reason" json:"reason"`
	Until  string `yaml:"until,omitempty" json:"until,omitempty"`
}

// Accepted is one UNVERIFIED message that matched an accepted entry.
type Accepted struct {
	Check   string `yaml:"check" json:"check"`
	Message string `yaml:"message" json:"message"`
	Reason  string `yaml:"reason" json:"reason"`
	Until   string `yaml:"until,omitempty" json:"until,omitempty"`
}

// ParseAccepted reads the accepted-unverified file. It accepts either the
// documented "accepted:" mapping or a bare list.
func ParseAccepted(data []byte) ([]AcceptedEntry, error) {
	var wrapper struct {
		Accepted []AcceptedEntry `yaml:"accepted"`
	}
	if err := yaml.Unmarshal(data, &wrapper); err == nil && len(wrapper.Accepted) > 0 {
		return wrapper.Accepted, nil
	}
	var list []AcceptedEntry
	if err := yaml.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ApplyAccepted splits one check's UNVERIFIED messages into those a matching
// accepted entry covers and the rest. Entries whose check differs, whose match
// is absent, or whose until date has passed do not accept anything; expired
// entries for check are returned so the caller can warn about them.
func ApplyAccepted(check string, unverified []string, entries []AcceptedEntry, now time.Time) (remaining []string, accepted []Accepted, expired []AcceptedEntry) {
	remaining = make([]string, 0, len(unverified))
	for _, entry := range entries {
		if !strings.EqualFold(strings.TrimSpace(entry.Check), strings.TrimSpace(check)) {
			continue
		}
		if acceptedEntryExpired(entry, now) {
			expired = append(expired, entry)
		}
	}
	for _, message := range unverified {
		matched := false
		for _, entry := range entries {
			if !strings.EqualFold(strings.TrimSpace(entry.Check), strings.TrimSpace(check)) {
				continue
			}
			if acceptedEntryExpired(entry, now) || strings.TrimSpace(entry.Match) == "" {
				continue
			}
			if !strings.Contains(message, entry.Match) {
				continue
			}
			accepted = append(accepted, Accepted{
				Check:   check,
				Message: message,
				Reason:  entry.Reason,
				Until:   entry.Until,
			})
			matched = true
			break
		}
		if !matched {
			remaining = append(remaining, message)
		}
	}
	return remaining, accepted, expired
}

// acceptedEntryExpired reports whether until is before now's UTC date. An
// absent until never expires; a malformed one is treated as expired so a typo
// cannot silently accept a finding forever.
func acceptedEntryExpired(entry AcceptedEntry, now time.Time) bool {
	until := strings.TrimSpace(entry.Until)
	if until == "" {
		return false
	}
	if _, err := time.Parse("2006-01-02", until); err != nil {
		return true
	}
	return now.UTC().Format("2006-01-02") > until
}
