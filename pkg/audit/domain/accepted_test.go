package domain

import (
	"testing"
	"time"
)

func TestParseAcceptedReadsWrapperAndBareList(t *testing.T) {
	wrapped, err := ParseAccepted([]byte("accepted:\n  - check: agents\n    match: cursor CLI not installed\n    reason: factory editors\n    until: 2026-12-31\n"))
	if err != nil {
		t.Fatalf("parse wrapper: %v", err)
	}
	if len(wrapped) != 1 || wrapped[0].Check != "agents" || wrapped[0].Until != "2026-12-31" {
		t.Fatalf("wrapped = %+v", wrapped)
	}
	bare, err := ParseAccepted([]byte("- check: deps\n  match: is not installed\n  reason: offline host\n"))
	if err != nil {
		t.Fatalf("parse bare list: %v", err)
	}
	if len(bare) != 1 || bare[0].Check != "deps" {
		t.Fatalf("bare = %+v", bare)
	}
}

func TestApplyAcceptedMatchesCheckAndSubstring(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	entries := []AcceptedEntry{
		{Check: "agents", Match: "cursor CLI not installed", Reason: "factory editors"},
		{Check: "deps", Match: "cursor", Reason: "other check"},
	}
	unverified := []string{
		"agents: cursor CLI not installed (runtime status unverified)",
		"agents: codex CLI not installed (runtime status unverified)",
	}

	remaining, accepted, expired := ApplyAccepted("agents", unverified, entries, now)
	if len(expired) != 0 {
		t.Errorf("expired = %+v, want none", expired)
	}
	if len(accepted) != 1 || accepted[0].Reason != "factory editors" {
		t.Fatalf("accepted = %+v", accepted)
	}
	if accepted[0].Message != unverified[0] {
		t.Errorf("accepted message = %q, want %q", accepted[0].Message, unverified[0])
	}
	if len(remaining) != 1 || remaining[0] != unverified[1] {
		t.Errorf("remaining = %+v, want the codex message", remaining)
	}
}

func TestApplyAcceptedIgnoresExpiredEntries(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	entries := []AcceptedEntry{
		{Check: "agents", Match: "cursor CLI not installed", Reason: "expired", Until: "2026-10-01"},
		{Check: "agents", Match: "codex CLI not installed", Reason: "last day", Until: "2026-11-02"},
	}
	unverified := []string{
		"agents: cursor CLI not installed (runtime status unverified)",
		"agents: codex CLI not installed (runtime status unverified)",
	}

	remaining, accepted, expired := ApplyAccepted("agents", unverified, entries, now)
	if len(accepted) != 1 || accepted[0].Reason != "last day" {
		t.Fatalf("accepted = %+v, want only the entry valid through today", accepted)
	}
	if len(remaining) != 1 || remaining[0] != unverified[0] {
		t.Errorf("remaining = %+v, want the expired cursor message", remaining)
	}
	if len(expired) != 1 || expired[0].Reason != "expired" {
		t.Errorf("expired = %+v", expired)
	}
}

func TestApplyAcceptedTreatsMalformedUntilAsExpired(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	entries := []AcceptedEntry{{Check: "agents", Match: "cursor", Reason: "bad date", Until: "soon"}}

	remaining, accepted, expired := ApplyAccepted("agents", []string{"agents: cursor missing"}, entries, now)
	if len(accepted) != 0 || len(remaining) != 1 || len(expired) != 1 {
		t.Errorf("remaining=%+v accepted=%+v expired=%+v", remaining, accepted, expired)
	}
}
