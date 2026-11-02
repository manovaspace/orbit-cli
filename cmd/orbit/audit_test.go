package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	"github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/output"
)

// auditFixtureRoot locates the audit package workspace fixture.
func auditFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "pkg", "audit", "testdata", "workspace"))
	if err != nil {
		t.Fatalf("fixture root: %v", err)
	}
	return root
}

// staticAuditWorkspace pins the repo list so command tests are deterministic.
type staticAuditWorkspace []application.Repo

func (w staticAuditWorkspace) Repos(string, string) ([]application.Repo, error) { return w, nil }

// quietVCS reports no origin tracking ref so clone checks stay silent.
type quietVCS struct{}

func (quietVCS) Head(string, string) (string, string, error) { return "", "", nil }

// stubAuditService replaces the wiring for one command test run.
func stubAuditService(t *testing.T, repos []application.Repo) {
	t.Helper()
	root := auditFixtureRoot(t)
	previousService, previousPaths := auditNewService, auditWorkspacePaths
	t.Cleanup(func() { auditNewService, auditWorkspacePaths = previousService, previousPaths })

	auditWorkspacePaths = func() (string, string) { return root, filepath.Join(root, "workspace.yaml") }
	auditNewService = func(root, manifestPath string) *application.Service {
		service := audit.NewService(root, manifestPath)
		service.Workspace = staticAuditWorkspace(repos)
		service.VCS = quietVCS{}
		return service
	}
}

func executeAudit(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	cmd := newAuditCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	return out, cmd.Execute()
}

func TestCleanExitsZero(t *testing.T) {
	root := auditFixtureRoot(t)
	stubAuditService(t, []application.Repo{{
		Name: "clean", Path: "orbit/clean", Dir: filepath.Join(root, "orbit", "clean"), DefaultBranch: "main",
	}})

	out, err := executeAudit(t, "versions")
	if err != nil {
		t.Fatalf("audit versions on a clean workspace: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("clean run printed %d lines, want only the summary:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "summary: 0 findings") {
		t.Errorf("summary = %q", lines[0])
	}
}

func TestJSONSchemaVersion(t *testing.T) {
	root := auditFixtureRoot(t)
	stubAuditService(t, []application.Repo{{
		Name: "clean", Path: "orbit/clean", Dir: filepath.Join(root, "orbit", "clean"), DefaultBranch: "main",
	}})

	out, err := executeAudit(t, "versions", "--json")
	if err != nil {
		t.Fatalf("audit versions --json: %v\n%s", err, out)
	}
	var envelope output.Envelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", envelope.SchemaVersion)
	}
	if !envelope.OK {
		t.Fatalf("envelope = %+v", envelope)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("re-encode data: %v", err)
	}
	var report audit.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, data)
	}
	if report.SchemaVersion != 1 {
		t.Errorf("report schemaVersion = %d, want 1", report.SchemaVersion)
	}
}

func TestAuditFindingsExitOne(t *testing.T) {
	stubAuditService(t, nil)
	auditNewService = func(root, manifestPath string) *application.Service {
		service := audit.NewService(root, manifestPath)
		service.VCS = quietVCS{}
		return service
	}

	out, err := executeAudit(t, "versions")
	if err == nil {
		t.Fatalf("drifting fixture exited 0:\n%s", out)
	}
	if got := exitCodeFor(err); got != 1 {
		t.Errorf("exit code = %d, want 1\n%s", got, out)
	}
}

func TestAuditUpstreamWithoutRemoteExitsTwo(t *testing.T) {
	stubAuditService(t, nil)

	out, err := executeAudit(t, "upstream")
	if err == nil {
		t.Fatalf("upstream without --remote exited 0:\n%s", out)
	}
	if got := exitCodeFor(err); got != 2 {
		t.Errorf("exit code = %d, want 2\n%s", got, out)
	}
	if !strings.Contains(out.String(), "remote checks need --remote") {
		t.Errorf("output does not carry the named reason:\n%s", out)
	}
}

func TestAuditMergesRejectsNonPositiveDays(t *testing.T) {
	stubAuditService(t, nil)

	_, err := executeAudit(t, "merges", "--days", "0")
	if err == nil {
		t.Fatal("merges --days 0 exited 0")
	}
	if got := exitCodeFor(err); got != 3 {
		t.Errorf("exit code = %d, want 3", got)
	}
}

func TestAuditJSONFindingsCarryData(t *testing.T) {
	root := auditFixtureRoot(t)
	stubAuditService(t, []application.Repo{{
		Name: "go-drift", Path: "orbit/go-drift", Dir: filepath.Join(root, "orbit", "go-drift"), DefaultBranch: "main",
	}})

	out, err := executeAudit(t, "versions", "--json")
	if err == nil {
		t.Fatalf("drifting fixture exited 0")
	}
	if got := exitCodeFor(err); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
	var envelope output.Envelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope.OK || envelope.SchemaVersion != 1 || envelope.Data == nil {
		t.Fatalf("failure envelope = %+v", envelope)
	}
}
