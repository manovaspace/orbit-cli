package audit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// evalCall is one command the fake runner saw.
type evalCall struct {
	dir  string
	line string
}

// evalWritingRunner emulates the handbook scripts. The eval writes
// eval-results.json into its working directory, exactly like eval.mjs.
type evalWritingRunner struct {
	calls []evalCall
}

func (r *evalWritingRunner) Run(_ context.Context, dir, name string, args ...string) (runner.Result, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, evalCall{dir: dir, line: line})
	switch {
	case strings.Contains(line, "check-all.mjs"):
		return runner.Result{Stdout: `{"schemaVersion":1,"ok":true,"findings":[],"unverified":[]}`}, nil
	case strings.Contains(line, "setup-mcp.py --check"):
		return runner.Result{}, nil
	case strings.Contains(line, "eval.mjs"):
		if _, err := os.Stat(filepath.Join(dir, "eval.mjs")); err != nil {
			return runner.Result{}, fmt.Errorf("eval.mjs not copied into %s: %w", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "eval-results.json"), []byte("rewritten\n"), 0o644); err != nil {
			return runner.Result{}, err
		}
		return runner.Result{Stdout: "eval: recall@5=0.93 mrr=0.91 (14/15)\neval: OK\n"}, nil
	}
	return runner.Result{}, fmt.Errorf("unexpected command: %s", line)
}

func writeAuditFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestAgentsEvalLeavesNoFilesInTheHandbook(t *testing.T) {
	root := t.TempDir()
	writeAuditFixture(t, root, "handbook/agents/scripts/check-all.mjs", "// fixture\n")
	writeAuditFixture(t, root, "handbook/agents/setup-mcp.py", "# fixture\n")
	writeAuditFixture(t, root, "handbook/agents/opencode/manova-implementer.md", "---\n---\n")
	writeAuditFixture(t, root, "handbook/agents/rules/rtk.mdc", "RTK\n")
	writeAuditFixture(t, root, "handbook/packages/handbook-mcp/eval.mjs", "// fixture\n")
	writeAuditFixture(t, root, "handbook/packages/handbook-mcp/eval-results.json", "old results\n")

	resultsPath := filepath.Join(root, filepath.FromSlash("handbook/packages/handbook-mcp/eval-results.json"))
	before, err := os.Stat(resultsPath)
	if err != nil {
		t.Fatalf("stat eval results: %v", err)
	}

	commands := &evalWritingRunner{}
	service := NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Commands = runnerCommands{runner: commands}
	_ = service.Agents(context.Background())

	after, err := os.Stat(resultsPath)
	if err != nil {
		t.Fatalf("stat eval results after audit: %v", err)
	}
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		t.Fatalf("read eval results after audit: %v", err)
	}
	if string(data) != "old results\n" {
		t.Errorf("eval results content = %q, want the original", data)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("eval results mtime moved: %s -> %s", before.ModTime(), after.ModTime())
	}

	isolated := ""
	for _, call := range commands.calls {
		if !strings.Contains(call.line, "eval.mjs") {
			continue
		}
		if call.dir == filepath.Dir(resultsPath) {
			t.Errorf("eval ran in the handbook checkout: %s", call.dir)
		}
		isolated = call.dir
	}
	if isolated == "" {
		t.Fatal("the audit never ran eval.mjs")
	}
	if _, err := os.Stat(isolated); !os.IsNotExist(err) {
		t.Errorf("isolated eval copy %s was not removed", isolated)
	}
}
