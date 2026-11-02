package audit_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	"github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/audit/infrastructure/npmhttp"
)

// fixtureRoot is the workspace fixture shared by the audit tests.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", "workspace"))
	if err != nil {
		t.Fatalf("fixture root: %v", err)
	}
	return root
}

// fixtureService wires the real adapters against the fixture workspace.
func fixtureService(t *testing.T) *application.Service {
	t.Helper()
	root := fixtureRoot(t)
	return audit.NewService(root, filepath.Join(root, "workspace.yaml"))
}

// staticWorkspace replaces the manifest with an explicit repo list.
type staticWorkspace []application.Repo

func (w staticWorkspace) Repos(string, string) ([]application.Repo, error) { return w, nil }

// fakeVCS answers git ref reads without touching the filesystem.
type fakeVCS struct {
	head   string
	origin string
	err    error
}

func (v fakeVCS) Head(string, string) (string, string, error) { return v.head, v.origin, v.err }

// failingRegistry fails the test if anything asks it for a remote version.
type failingRegistry struct {
	t     *testing.T
	calls *int32
}

func (r failingRegistry) Latest(context.Context, string) (string, error) {
	atomic.AddInt32(r.calls, 1)
	r.t.Error("registry called without --remote")
	return "", errors.New("offline")
}

func findFinding(report audit.Report, id, contains string) *audit.Finding {
	for i := range report.Findings {
		f := report.Findings[i]
		if f.ID != id {
			continue
		}
		if contains == "" || strings.Contains(f.Subject+" "+f.Actual+" "+f.Expected+" "+f.Fix, contains) {
			return &report.Findings[i]
		}
	}
	return nil
}

func countFindings(report audit.Report, id string) int {
	count := 0
	for _, f := range report.Findings {
		if f.ID == id {
			count++
		}
	}
	return count
}

func TestVersionsFlagsGoDrift(t *testing.T) {
	result := fixtureService(t).Versions(context.Background())

	f := findFinding(result.Report, "versions.go", "orbit/go-drift")
	if f == nil {
		t.Fatalf("no versions.go finding for orbit/go-drift: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityError {
		t.Errorf("severity = %s, want ERROR", f.Severity)
	}
	if f.Actual != "1.26.0" || f.Expected != "1.26.7" {
		t.Errorf("finding = %+v, want actual 1.26.0 expected 1.26.7", f)
	}
	if !strings.Contains(f.Fix, "orbit/go-drift/go.mod") {
		t.Errorf("fix does not name the file: %q", f.Fix)
	}
}

func TestVersionsFlagsPackageManagerDrift(t *testing.T) {
	result := fixtureService(t).Versions(context.Background())

	f := findFinding(result.Report, "versions.bun", "clients/demo/bun-drift")
	if f == nil {
		t.Fatalf("no versions.bun finding for clients/demo/bun-drift: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityError {
		t.Errorf("severity = %s, want ERROR", f.Severity)
	}
	if f.Actual != "1.3.0" || f.Expected != "1.4.0" {
		t.Errorf("finding = %+v, want actual 1.3.0 expected 1.4.0", f)
	}
}

func TestVersionsFlagsNextForm(t *testing.T) {
	result := fixtureService(t).Versions(context.Background())

	f := findFinding(result.Report, "versions.next", "clients/demo/next-caret")
	if f == nil {
		t.Fatalf("no versions.next finding for clients/demo/next-caret: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityError {
		t.Errorf("severity = %s, want ERROR", f.Severity)
	}
	if f.Actual != "^16.4.3" || f.Expected != "16.4.3" {
		t.Errorf("finding = %+v, want the caret form reported against the exact pin", f)
	}
	if !strings.Contains(f.Fix, "clients/demo/next-caret/package.json") {
		t.Errorf("fix does not name the manifest: %q", f.Fix)
	}
}

func TestVersionsExceptionDowngradesToInfo(t *testing.T) {
	result := fixtureService(t).Versions(context.Background())

	f := findFinding(result.Report, "versions.react", "clients/demo/react-caret")
	if f == nil {
		t.Fatalf("no versions.react finding for clients/demo/react-caret: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityInfo {
		t.Errorf("severity = %s, want INFO for an excepted drift", f.Severity)
	}
	if !strings.Contains(f.Fix, "excepted: deliberate caret") {
		t.Errorf("fix does not carry the exception reason: %q", f.Fix)
	}
}

func TestDepsFlagsMultipleResolved(t *testing.T) {
	result := fixtureService(t).Deps(context.Background(), "@manovaspace/ui", false)

	f := findFinding(result.Report, "deps.multiple", "clients/demo/multi-lock")
	if f == nil {
		t.Fatalf("no deps.multiple finding for clients/demo/multi-lock: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityWarn {
		t.Errorf("severity = %s, want WARN", f.Severity)
	}
	if !strings.Contains(f.Actual, "0.11.1") || !strings.Contains(f.Actual, "0.8.0") {
		t.Errorf("actual = %q, want both resolved versions", f.Actual)
	}
}

func TestDepsFlagsForbiddenRange(t *testing.T) {
	result := fixtureService(t).Deps(context.Background(), "@manovaspace/ui", false)

	for _, spec := range []string{"file:", "*", "latest"} {
		f := findFinding(result.Report, "deps.forbidden", spec)
		if f == nil {
			t.Fatalf("no deps.forbidden finding for %q: %+v", spec, result.Findings)
		}
		if f.Severity != audit.SeverityError {
			t.Errorf("%q severity = %s, want ERROR", spec, f.Severity)
		}
	}
	if got := countFindings(result.Report, "deps.forbidden"); got != 3 {
		t.Errorf("deps.forbidden findings = %d, want 3", got)
	}
}

func TestDepsShadow(t *testing.T) {
	result := fixtureService(t).Deps(context.Background(), "@manovaspace/ui", false)

	f := findFinding(result.Report, "deps.shadow", "Badge")
	if f == nil {
		t.Fatalf("no deps.shadow finding for Badge: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityWarn {
		t.Errorf("severity = %s, want WARN", f.Severity)
	}
	if f.Subject != "Badge" {
		t.Errorf("subject = %q, want the shadowed export", f.Subject)
	}
	if !strings.Contains(f.Fix, "clients/demo/shadow") {
		t.Errorf("fix does not name the repo: %q", f.Fix)
	}
	if findFinding(result.Report, "deps.shadow", "Sheet") == nil {
		t.Errorf("default-exported Sheet not reported: %+v", result.Findings)
	}
}

func TestDepsShadowOnlyForDeclaredKit(t *testing.T) {
	result := fixtureService(t).Deps(context.Background(), "@manovaspace/ui", false)

	joined := strings.Join(result.Unverified, "\n")
	if strings.Contains(joined, "clients/demo/ui-mention") {
		t.Errorf("repository that only mentions the kit is unverified: %q", joined)
	}
	if f := findFinding(result.Report, "deps.shadow", "clients/demo/ui-mention"); f != nil {
		t.Errorf("repository that only mentions the kit reported as shadowing: %+v", f)
	}
}

func TestDepsShadowSkipsCommonsLibraries(t *testing.T) {
	result := fixtureService(t).Deps(context.Background(), "@manovaspace/ui", false)

	for _, repoPath := range []string{"manovaspace/ts", "manovaspace/design-system"} {
		if f := findFinding(result.Report, "deps.shadow", repoPath); f != nil {
			t.Errorf("commons library %s reported as shadowing: %+v", repoPath, f)
		}
	}
}

func TestUsageResolvesWorkspaceMember(t *testing.T) {
	result := fixtureService(t).Usage(context.Background(), "", false)

	joined := strings.Join(result.Unverified, "\n")
	for _, unwanted := range []string{"workspace-member", "@manovaspace/versioning"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%s still unverified: %q", unwanted, joined)
		}
	}
	if f := findFinding(result.Report, "usage.unknown", "workspace-member"); f != nil {
		t.Errorf("workspace member exports not resolved: %+v", f)
	}
}

func TestUsageResolvesSubpathExport(t *testing.T) {
	result := fixtureService(t).Usage(context.Background(), "", false)

	joined := strings.Join(result.Unverified, "\n")
	for _, unwanted := range []string{"clients/demo/subpath", "@manovaspace/pwa/react"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%s still unverified: %q", unwanted, joined)
		}
	}
	if f := findFinding(result.Report, "usage.unknown", "persistent-providers"); f != nil {
		t.Errorf("subpath exports not resolved: %+v", f)
	}
}

func TestMissingRepoIsUnverified(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{{
		Name: "ghost", Path: "orbit/ghost", Dir: filepath.Join(root, "orbit", "ghost"), DefaultBranch: "main",
	}}

	result := service.Versions(context.Background())
	if result.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2: %+v", result.ExitCode(), result.Report)
	}
	joined := strings.Join(result.Unverified, "\n")
	if !strings.Contains(joined, "orbit/ghost") {
		t.Errorf("unverified does not name the repo: %q", joined)
	}
}

func TestBehindCloneWarns(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{{
		Name: "clean", Path: "orbit/clean", Dir: filepath.Join(root, "orbit", "clean"), DefaultBranch: "main",
	}}
	service.VCS = fakeVCS{head: "1111111", origin: "2222222"}
	var calls int32
	service.Registry = failingRegistry{t: t, calls: &calls}

	result := service.Versions(context.Background())

	f := findFinding(result.Report, "clone.behind", "orbit/clean")
	if f == nil {
		t.Fatalf("no clone.behind finding: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityWarn {
		t.Errorf("severity = %s, want WARN", f.Severity)
	}
	if !strings.Contains(f.Subject, "clean") {
		t.Errorf("subject = %q, want the repo named", f.Subject)
	}
	if result.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0 for a warning", result.ExitCode())
	}
	if calls != 0 {
		t.Errorf("registry calls = %d, want none", calls)
	}
}

func TestUsageNamespaceImportUnverified(t *testing.T) {
	result := fixtureService(t).Usage(context.Background(), "", false)

	joined := strings.Join(result.Unverified, "\n")
	if !strings.Contains(joined, "usage-ns") || !strings.Contains(joined, "namespace") {
		t.Fatalf("namespace import not unverified: %q", joined)
	}
	if result.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", result.ExitCode())
	}
}

func TestUsageFlagsUnknownSymbol(t *testing.T) {
	result := fixtureService(t).Usage(context.Background(), "", false)

	f := findFinding(result.Report, "usage.unknown", "Nope")
	if f == nil {
		t.Fatalf("no usage.unknown finding for Nope: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityWarn {
		t.Errorf("severity = %s, want WARN", f.Severity)
	}
	if !strings.Contains(f.Fix, "clients/demo/usage-named") {
		t.Errorf("fix does not name the source file: %q", f.Fix)
	}
}

func TestUsageUnusedListsKitExports(t *testing.T) {
	result := fixtureService(t).Usage(context.Background(), "", true)

	f := findFinding(result.Report, "usage.unused", "Sheet")
	if f == nil {
		t.Fatalf("no usage.unused finding for Sheet: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityInfo {
		t.Errorf("severity = %s, want INFO", f.Severity)
	}
	if badge := findFinding(result.Report, "usage.unused", "Badge"); badge != nil {
		t.Errorf("namespace-imported Badge reported unused: %+v", badge)
	}
}

func TestDepsRemoteReadsRegistry(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if !strings.Contains(r.URL.Path, "@manovaspace/ui") {
			t.Errorf("registry path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dist-tags":{"latest":"0.12.0"}}`))
	}))
	defer server.Close()

	service := fixtureService(t)
	service.Registry = npmhttp.New(server.URL)
	result := service.Deps(context.Background(), "@manovaspace/ui", true)

	if hits == 0 {
		t.Fatal("--remote did not read the registry")
	}
	f := findFinding(result.Report, "deps.outdated", "0.12.0")
	if f == nil {
		t.Fatalf("no deps.outdated finding: %+v", result.Findings)
	}
	if f.Expected != "0.12.0" {
		t.Errorf("expected = %q, want the registry latest", f.Expected)
	}
}

func TestDepsWithoutRemoteMakesNoRequest(t *testing.T) {
	service := fixtureService(t)
	var calls int32
	service.Registry = failingRegistry{t: t, calls: &calls}

	service.Deps(context.Background(), "@manovaspace/ui", false)

	if calls != 0 {
		t.Errorf("registry calls = %d, want none without --remote", calls)
	}
}
