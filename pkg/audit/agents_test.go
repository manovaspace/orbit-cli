package audit_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	"github.com/manovaspace/orbit-cli/pkg/audit/application"
)

func writeFixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func agentWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixtureFile(t, root, "handbook/agents/scripts/check-all.mjs", "// fixture\n")
	writeFixtureFile(t, root, "handbook/agents/setup-mcp.py", "# fixture\n")
	writeFixtureFile(t, root, "handbook/agents/opencode/manova-implementer.md", "---\nname: implementer\n---\n")
	writeFixtureFile(t, root, "handbook/packages/handbook-mcp/eval.mjs", "// fixture\n")
	writeFixtureFile(t, root, "handbook/agents/rules/rtk.mdc", "RTK\n")
	return root
}

func agentCommands() *fakeCommands {
	return &fakeCommands{
		answers: []commandAnswer{
			{
				prefix: "bun handbook/agents/scripts/check-all.mjs",
				result: application.CommandResult{
					Stdout:   `{"schemaVersion":1,"ok":false,"findings":[{"check":"links","code":"broken-link","file":"docs/a.md","message":"-> missing"}],"unverified":[]}`,
					ExitCode: 1,
				},
				err: errors.New("exit status 1"),
			},
			{
				prefix: "python3 handbook/agents/setup-mcp.py --check",
				result: application.CommandResult{ExitCode: 0},
			},
			{
				prefix: "bun handbook/packages/handbook-mcp/eval.mjs",
				result: application.CommandResult{Stdout: "eval: recall@5=0.93 mrr=0.91 (14/15)\neval: OK\n", ExitCode: 0},
			},
		},
		paths: map[string]string{
			"claude":   "/usr/bin/claude",
			"opencode": "/usr/bin/opencode",
		},
	}
}

func TestAgentsPassesThroughCheckAllFailure(t *testing.T) {
	root := agentWorkspace(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Commands = agentCommands()

	result := service.Agents(context.Background())
	if result.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1: %+v", result.ExitCode(), result.Report)
	}
	f := findFinding(result.Report, "agents.check-all", "docs/a.md")
	if f == nil {
		t.Fatalf("no agents.check-all finding: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityError {
		t.Errorf("severity = %s, want ERROR", f.Severity)
	}
	if !strings.Contains(f.Actual, "broken-link") {
		t.Errorf("actual = %q, want the check-all code", f.Actual)
	}
}

func TestAgentsReportsMissingEditorRuntime(t *testing.T) {
	root := agentWorkspace(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Commands = agentCommands()

	result := service.Agents(context.Background())
	joined := strings.Join(result.Unverified, "\n")
	for _, editor := range []string{"cursor", "codex", "gemini"} {
		if !strings.Contains(joined, editor) {
			t.Errorf("missing %s runtime not unverified: %q", editor, joined)
		}
	}
}

func TestAgentsReportsMCPDrift(t *testing.T) {
	root := agentWorkspace(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	commands := agentCommands()
	commands.answers[1] = commandAnswer{
		prefix: "python3 handbook/agents/setup-mcp.py --check",
		result: application.CommandResult{Stdout: "DRIFT /tmp/.mcp.json\n", ExitCode: 1},
		err:    errors.New("exit status 1"),
	}
	service.Commands = commands

	result := service.Agents(context.Background())
	f := findFinding(result.Report, "agents.mcp-drift", "MCP")
	if f == nil {
		t.Fatalf("no agents.mcp-drift finding: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityWarn {
		t.Errorf("severity = %s, want WARN", f.Severity)
	}
}

func TestAgentsSkipsMissingEvalAsUnverified(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "handbook/agents/scripts/check-all.mjs", "// fixture\n")
	writeFixtureFile(t, root, "handbook/agents/setup-mcp.py", "# fixture\n")
	writeFixtureFile(t, root, "handbook/agents/opencode/manova-implementer.md", "---\n---\n")
	writeFixtureFile(t, root, "handbook/agents/rules/rtk.mdc", "RTK\n")
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Commands = &fakeCommands{
		answers: []commandAnswer{{
			prefix: "bun handbook/agents/scripts/check-all.mjs",
			result: application.CommandResult{Stdout: `{"schemaVersion":1,"ok":true,"findings":[],"unverified":[]}`},
		}, {
			prefix: "python3 handbook/agents/setup-mcp.py --check",
			result: application.CommandResult{ExitCode: 0},
		}},
		paths: map[string]string{"claude": "/usr/bin/claude", "opencode": "/usr/bin/opencode", "cursor": "/usr/bin/cursor", "codex": "/usr/bin/codex", "gemini": "/usr/bin/gemini"},
	}

	result := service.Agents(context.Background())
	joined := strings.Join(result.Unverified, "\n")
	if !strings.Contains(joined, "handbook-mcp") {
		t.Errorf("missing eval not unverified: %q", joined)
	}
}
