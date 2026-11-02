package audit_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	"github.com/manovaspace/orbit-cli/pkg/audit/application"
)

// acceptedAgentCommands answers every agent probe cleanly; the three factory
// editors stay missing.
func acceptedAgentCommands() *fakeCommands {
	return &fakeCommands{
		answers: []commandAnswer{
			{
				prefix: "bun handbook/agents/scripts/check-all.mjs",
				result: application.CommandResult{Stdout: `{"schemaVersion":1,"ok":true,"findings":[],"unverified":[]}`},
			},
			{
				prefix: "python3 handbook/agents/setup-mcp.py --check",
				result: application.CommandResult{ExitCode: 0},
			},
			{
				prefix: "bun handbook/packages/handbook-mcp/eval.mjs",
				result: application.CommandResult{Stdout: "eval: recall@5=0.93 mrr=0.91\n", ExitCode: 0},
			},
		},
		paths: map[string]string{
			"claude":   "/usr/bin/claude",
			"opencode": "/usr/bin/opencode",
		},
	}
}

// acceptedAgentService builds an agent workspace with the given accepted list.
func acceptedAgentService(t *testing.T, accepted string) *application.Service {
	t.Helper()
	root := agentWorkspace(t)
	writeFixtureFile(t, root, "handbook/agents/workspace/audit-accepted.yaml", accepted)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Commands = acceptedAgentCommands()
	service.Now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
	return service
}

func TestAgentsAcceptsNamedUnverified(t *testing.T) {
	service := acceptedAgentService(t, `accepted:
  - check: agents
    match: cursor CLI not installed
    reason: not installed on this host; first-class editors per the factory plan, runtime cannot be verified here
`)

	result := service.Agents(context.Background())
	if len(result.Accepted) != 1 {
		t.Fatalf("accepted = %+v, want the cursor entry", result.Accepted)
	}
	if !strings.Contains(result.Accepted[0].Message, "cursor CLI not installed") {
		t.Errorf("accepted message = %q", result.Accepted[0].Message)
	}
	if !strings.Contains(result.Accepted[0].Reason, "factory plan") {
		t.Errorf("accepted reason = %q", result.Accepted[0].Reason)
	}
	joined := strings.Join(result.Unverified, "\n")
	for _, editor := range []string{"cursor", "codex", "gemini"} {
		if editor == "cursor" {
			if strings.Contains(joined, "cursor CLI not installed") {
				t.Errorf("accepted cursor still unverified: %q", joined)
			}
			continue
		}
		if !strings.Contains(joined, editor) {
			t.Errorf("unaccepted %s missing from unverified: %q", editor, joined)
		}
	}
	if result.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2 while codex and gemini remain", result.ExitCode())
	}
}

func TestAgentsAcceptedUnverifiedExitsZero(t *testing.T) {
	service := acceptedAgentService(t, `accepted:
  - check: agents
    match: cursor CLI not installed
    reason: factory editors
  - check: agents
    match: codex CLI not installed
    reason: factory editors
  - check: agents
    match: gemini CLI not installed
    reason: factory editors
`)

	result := service.Agents(context.Background())
	if len(result.Accepted) != 3 {
		t.Fatalf("accepted = %+v, want three entries", result.Accepted)
	}
	if len(result.Unverified) != 0 {
		t.Fatalf("unverified = %+v, want none", result.Unverified)
	}
	if result.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode())
	}
}

func TestAgentsExpiredAcceptanceWarnsAndStaysUnverified(t *testing.T) {
	service := acceptedAgentService(t, `accepted:
  - check: agents
    match: cursor CLI not installed
    reason: factory editors
    until: 2020-01-01
`)

	result := service.Agents(context.Background())
	if len(result.Accepted) != 0 {
		t.Errorf("expired entry accepted: %+v", result.Accepted)
	}
	if !strings.Contains(strings.Join(result.Unverified, "\n"), "cursor CLI not installed") {
		t.Errorf("expired cursor acceptance hid the finding: %+v", result.Unverified)
	}
	f := findFinding(result.Report, "audit.accepted-expired", "cursor")
	if f == nil {
		t.Fatalf("no audit.accepted-expired warning: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityWarn {
		t.Errorf("severity = %s, want WARN", f.Severity)
	}
	if result.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", result.ExitCode())
	}
}

func TestAgentsAcceptanceRequiresMatchingCheck(t *testing.T) {
	service := acceptedAgentService(t, `accepted:
  - check: deps
    match: cursor CLI not installed
    reason: wrong check
`)

	result := service.Agents(context.Background())
	if len(result.Accepted) != 0 {
		t.Errorf("entry from another check accepted: %+v", result.Accepted)
	}
	if !strings.Contains(strings.Join(result.Unverified, "\n"), "cursor CLI not installed") {
		t.Errorf("cursor missing from unverified: %+v", result.Unverified)
	}
}
