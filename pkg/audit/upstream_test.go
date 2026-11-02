package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
	"github.com/manovaspace/orbit-cli/pkg/audit/infrastructure/osvhttp"
)

func TestUpstreamLagIsInfo(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace(nil)
	service.Registry = fakeRegistry{
		"bun":   "1.5.0",
		"next":  "16.4.3",
		"react": "19.3.0",
	}
	service.GitHub = &fakeGitHub{releases: map[string]string{
		"golang/go":   "go1.26.7",
		"nodejs/node": "v24.19.0",
	}}
	service.Advisories = &fakeAdvisories{}

	result := service.Upstream(context.Background(), true)
	if result.ExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0: %+v", result.ExitCode(), result.Report)
	}
	if got := countSeverity(&result, domain.SeverityInfo, "upstream.lag"); got != 2 {
		t.Fatalf("upstream.lag INFO findings = %d, want 2: %+v", got, result.Findings)
	}
	for _, subject := range []string{"bun", "node"} {
		f := findFinding(result.Report, "upstream.lag", subject)
		if f == nil {
			t.Errorf("no upstream.lag finding for %s: %+v", subject, result.Findings)
			continue
		}
		if f.Severity != audit.SeverityInfo {
			t.Errorf("%s severity = %s, want INFO", subject, f.Severity)
		}
	}
}

func TestUpstreamReportsOSVAdvisory(t *testing.T) {
	var query struct {
		Queries []struct {
			Package struct {
				Name      string `json:"name"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Version string `json:"version"`
		} `json:"queries"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/querybatch" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			t.Errorf("decode query: %v", err)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-test","summary":"bad"}]}]}`))
	}))
	defer server.Close()

	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{{
		Name: "vulnerable", Path: "orbit/vulnerable", Dir: filepath.Join(root, "orbit", "vulnerable"), DefaultBranch: "main",
	}}
	service.Registry = fakeRegistry{"bun": "1.4.0", "next": "16.4.3", "react": "19.3.0"}
	service.GitHub = &fakeGitHub{releases: map[string]string{
		"golang/go":   "go1.27.3",
		"nodejs/node": "v24.18.0",
	}}
	service.Advisories = osvhttp.New(server.URL)

	result := service.Upstream(context.Background(), true)
	if result.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1: %+v", result.ExitCode(), result.Report)
	}
	f := findFinding(result.Report, "advisories.vulnerable", "github.com/example/vulnerable")
	if f == nil {
		t.Fatalf("no advisory finding: %+v", result.Findings)
	}
	if f.Severity != audit.SeverityError {
		t.Errorf("severity = %s, want ERROR", f.Severity)
	}
	if !strings.Contains(f.Actual, "GHSA-test") {
		t.Errorf("actual = %q, want the advisory id", f.Actual)
	}
	found := false
	for _, q := range query.Queries {
		if q.Package.Name == "github.com/example/vulnerable" && q.Package.Ecosystem == "Go" && q.Version == "v1.2.3" {
			found = true
		}
	}
	if !found {
		t.Errorf("OSV query did not carry the resolved Go module: %+v", query.Queries)
	}
}

func TestUpstreamWithoutRemoteMakesNoRequest(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace(nil)
	service.Registry = failingRegistry{t: t, calls: new(int32)}
	service.GitHub = &fakeGitHub{releaseErr: errors.New("no network")}
	service.Advisories = &fakeAdvisories{err: errors.New("no network")}

	result := service.Upstream(context.Background(), false)
	if result.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2: %+v", result.ExitCode(), result.Report)
	}
	joined := strings.Join(result.Unverified, "\n")
	if !strings.Contains(joined, "remote checks need --remote") {
		t.Errorf("unverified = %q, want the named reason", joined)
	}
}

func TestAllWithoutRemoteSkipsNetworkChecks(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace(nil)
	service.VCS = fakeVCS{}
	var calls int32
	service.Registry = failingRegistry{t: t, calls: &calls}
	service.GitHub = &fakeGitHub{releaseErr: errors.New("no network"), usageErr: errors.New("no network")}
	service.Advisories = &fakeAdvisories{err: errors.New("no network")}

	_ = service.All(context.Background(), false)
	if calls != 0 {
		t.Errorf("registry calls = %d, want none without --remote", calls)
	}
}

func TestUpstreamReportsAdvisoryOnResolvedVersion(t *testing.T) {
	root := fixtureRoot(t)
	service := audit.NewService(root, filepath.Join(root, "workspace.yaml"))
	service.Workspace = staticWorkspace{{
		Name: "vulnerable", Path: "orbit/vulnerable", Dir: filepath.Join(root, "orbit", "vulnerable"), DefaultBranch: "main",
	}}
	service.Registry = fakeRegistry{"bun": "1.4.0", "next": "16.4.3", "react": "19.3.0"}
	service.GitHub = &fakeGitHub{releases: map[string]string{
		"golang/go":   "go1.27.3",
		"nodejs/node": "v24.18.0",
	}}
	service.Advisories = &fakeAdvisories{hits: []domain.AdvisoryHit{{
		Package:    domain.PackageRef{Name: "lodash", Version: "4.17.20", Ecosystem: "npm"},
		Advisories: []domain.Advisory{{ID: "GHSA-lodash", Summary: "prototype pollution"}},
	}}}

	result := service.Upstream(context.Background(), true)
	if result.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1: %+v", result.ExitCode(), result.Report)
	}
	if findFinding(result.Report, "advisories.vulnerable", "lodash@4.17.20") == nil {
		t.Errorf("no advisory finding for lodash@4.17.20: %+v", result.Findings)
	}
}

func countSeverity(result *audit.Result, severity domain.Severity, id string) int {
	count := 0
	for _, f := range result.Findings {
		if f.ID == id && f.Severity == severity {
			count++
		}
	}
	return count
}
