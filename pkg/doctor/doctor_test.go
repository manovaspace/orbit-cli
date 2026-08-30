package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/host"
)

func TestParseVersionString(t *testing.T) {
	v, err := parseSemver("go version go1.23.1 linux/amd64", "go")
	if err != nil || v < "1.23" {
		t.Errorf("expected parsed go version >= 1.23, got %s (err: %v)", v, err)
	}

	tests := []struct {
		name     string
		raw      string
		tool     string
		expected string
		wantErr  bool
	}{
		{
			name:     "Go 1.26.0",
			raw:      "go version go1.26.0 linux/amd64",
			tool:     "go",
			expected: "1.26.0",
			wantErr:  false,
		},
		{
			name:     "Go 1.23.1",
			raw:      "go version go1.23.1 linux/amd64",
			tool:     "go",
			expected: "1.23.1",
			wantErr:  false,
		},
		{
			name:     "Node v24.18.0",
			raw:      "v24.18.0\n",
			tool:     "node",
			expected: "24.18.0",
			wantErr:  false,
		},
		{
			name:     "Node v22.8.0",
			raw:      "v22.8.0",
			tool:     "node",
			expected: "22.8.0",
			wantErr:  false,
		},
		{
			name:     "Bun 1.3.14",
			raw:      "1.3.14\n",
			tool:     "bun",
			expected: "1.3.14",
			wantErr:  false,
		},
		{
			name:     "Bun 1.1.0",
			raw:      "1.1.0",
			tool:     "bun",
			expected: "1.1.0",
			wantErr:  false,
		},
		{
			name:     "Caddy v2.8.4",
			raw:      "v2.8.4 h1:DqdHgkP18Ebgblm2c+3gP57y3sL10\n",
			tool:     "caddy",
			expected: "2.8.4",
			wantErr:  false,
		},
		{
			name:     "Typst 0.15.1",
			raw:      "typst 0.15.1 (unknown commit)\n",
			tool:     "typst",
			expected: "0.15.1",
			wantErr:  false,
		},
		{
			name:     "Docker Compose v2.40.3",
			raw:      "Docker Compose version 2.40.3+ds1-0ubuntu1\n",
			tool:     "docker-compose",
			expected: "2.40.3",
			wantErr:  false,
		},
		{
			name:     "Empty version",
			raw:      "",
			tool:     "go",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "Invalid output",
			raw:      "unrecognized output format",
			tool:     "go",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSemver(tc.raw, tc.tool)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got version %q", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.23.1", "1.23.0", 1},
		{"1.23.0", "1.23", 0},
		{"1.23", "1.23.0", 0},
		{"1.22.9", "1.23.0", -1},
		{"1.26.0", "1.23.0", 1},
		{"24.18.0", "22.0.0", 1},
		{"22.0.0", "20.0.0", 1},
		{"20.0.0", "20.0.0", 0},
		{"18.19.0", "20.0.0", -1},
		{"1.3.14", "1.1.0", 1},
		{"1.1.0", "1.1.0", 0},
		{"1.0.0", "1.1.0", -1},
		{"1.2.0", "1.1.0", 1},
	}

	for _, tc := range tests {
		res := CompareVersions(tc.v1, tc.v2)
		if res != tc.expected {
			t.Errorf("CompareVersions(%q, %q) = %d; expected %d", tc.v1, tc.v2, res, tc.expected)
		}
	}
}

func TestCheckHostFromReport(t *testing.T) {
	okReport := host.Report{OK: true}
	okRes := hostResult(okReport)
	if okRes.Status != StatusOK {
		t.Errorf("expected StatusOK, got %v", okRes.Status)
	}
	if okRes.Category != "System" || okRes.Name != "Host" {
		t.Errorf("expected System/Host, got %s/%s", okRes.Category, okRes.Name)
	}
	if okRes.Message != "Supported host: Ubuntu 24.04/26.04 LTS amd64, zsh, ~/.local/bin" {
		t.Errorf("unexpected OK message: %q", okRes.Message)
	}

	failReport := host.Report{
		OK: false,
		Failures: []host.Failure{
			{Code: "os", Message: "darwin is not supported"},
		},
	}
	failRes := hostResult(failReport)
	if failRes.Status != StatusError {
		t.Errorf("expected StatusError, got %v", failRes.Status)
	}
	if failRes.Category != "System" || failRes.Name != "Host" {
		t.Errorf("expected System/Host, got %s/%s", failRes.Category, failRes.Name)
	}
	wantMsg := strings.TrimSpace(host.Format(failReport))
	if failRes.Message != wantMsg {
		t.Errorf("expected message %q, got %q", wantMsg, failRes.Message)
	}
	if failRes.FixSuggestion == "" {
		t.Error("expected non-empty FixSuggestion on failure")
	}
}

func TestEvaluateGoVersion(t *testing.T) {
	// 1. Success case >= 1.26.0
	res := EvaluateGoVersion("go version go1.26.0 linux/amd64", nil)
	if res.Status != StatusOK {
		t.Errorf("expected StatusOK for go1.26.0, got %v", res.Status)
	}
	if !strings.Contains(res.Message, ">= 1.26.0 required") {
		t.Errorf("expected OK message to mention >= 1.26.0 required, got %q", res.Message)
	}

	resNext := EvaluateGoVersion("go version go1.26.2 linux/amd64", nil)
	if resNext.Status != StatusOK {
		t.Errorf("expected StatusOK for go1.26.2, got %v", resNext.Status)
	}

	// 2. Outdated case < 1.26.0 (e.g. 1.25.9, 1.24.2)
	resOutdated := EvaluateGoVersion("go version go1.24.2 linux/amd64", nil)
	if resOutdated.Status != StatusError {
		t.Errorf("expected StatusError for go1.24.2, got %v", resOutdated.Status)
	}
	if !strings.Contains(resOutdated.Message, "below required version") {
		t.Errorf("expected error message to mention below required version, got %q", resOutdated.Message)
	}

	res25 := EvaluateGoVersion("go version go1.25.9 linux/amd64", nil)
	if res25.Status != StatusError {
		t.Errorf("expected StatusError for go1.25.9, got %v", res25.Status)
	}

	// 3. Below 1.26.0 (e.g. 1.23.1)
	resOld := EvaluateGoVersion("go version go1.23.1 linux/amd64", nil)
	if resOld.Status != StatusError {
		t.Errorf("expected StatusError for go1.23.1, got %v", resOld.Status)
	}

	// 4. Exec error case
	resErr := EvaluateGoVersion("", errors.New("exec: not found"))
	if resErr.Status != StatusError {
		t.Errorf("expected StatusError for missing binary, got %v", resErr.Status)
	}
	if !strings.Contains(resErr.FixSuggestion, "1.26.0") {
		t.Errorf("expected missing binary fix suggestion to mention 1.26.0, got %q", resErr.FixSuggestion)
	}

	// 5. Unparseable output
	resUnparseable := EvaluateGoVersion("custom go build without semver", nil)
	if resUnparseable.Status != StatusWarning {
		t.Errorf("expected StatusWarning for unparseable output, got %v", resUnparseable.Status)
	}
}

func TestEvaluateNodeAndBunVersions(t *testing.T) {
	// Node tests
	node24 := EvaluateNodeVersion("v24.18.0\n", nil)
	if node24.Status != StatusOK {
		t.Errorf("expected StatusOK for Node v24.18.0, got %v", node24.Status)
	}

	node26 := EvaluateNodeVersion("v26.0.0\n", nil)
	if node26.Status != StatusOK {
		t.Errorf("expected StatusOK for Node v26.0.0, got %v", node26.Status)
	}

	node22 := EvaluateNodeVersion("v22.14.0\n", nil)
	if node22.Status != StatusError {
		t.Errorf("expected StatusError for Node v22.14.0, got %v", node22.Status)
	}

	node20 := EvaluateNodeVersion("v20.10.0\n", nil)
	if node20.Status != StatusError {
		t.Errorf("expected StatusError for Node v20.10.0, got %v", node20.Status)
	}

	nodeMissing := EvaluateNodeVersion("", errors.New("not found"))
	if nodeMissing.Status != StatusError {
		t.Errorf("expected StatusError for missing node, got %v", nodeMissing.Status)
	}

	// Bun tests
	bunOk := EvaluateBunVersion("1.4.0\n", nil)
	if bunOk.Status != StatusOK {
		t.Errorf("expected StatusOK for Bun 1.4.0, got %v", bunOk.Status)
	}

	bun11 := EvaluateBunVersion("1.1.0\n", nil)
	if bun11.Status != StatusError {
		t.Errorf("expected StatusError for Bun 1.1.0, got %v", bun11.Status)
	}

	bun15 := EvaluateBunVersion("1.5.0\n", nil)
	if bun15.Status != StatusError {
		t.Errorf("expected StatusError for Bun 1.5.0, got %v", bun15.Status)
	}

	bunMissing := EvaluateBunVersion("", errors.New("not found"))
	if bunMissing.Status != StatusError {
		t.Errorf("expected StatusError for missing Bun, got %v", bunMissing.Status)
	}
}

func TestEvaluateDocker(t *testing.T) {
	daemonOk := EvaluateDockerDaemon("Client: Docker Engine\nServer: Docker Engine\n", nil)
	if daemonOk.Status != StatusOK {
		t.Errorf("expected StatusOK for running daemon, got %v", daemonOk.Status)
	}

	daemonErr := EvaluateDockerDaemon("", errors.New("Cannot connect to the Docker daemon"))
	if daemonErr.Status != StatusError {
		t.Errorf("expected StatusError for stopped daemon, got %v", daemonErr.Status)
	}

	composeOk := EvaluateDockerCompose("Docker Compose version 2.40.3\n", nil)
	if composeOk.Status != StatusOK {
		t.Errorf("expected StatusOK for compose v2, got %v", composeOk.Status)
	}

	composeErr := EvaluateDockerCompose("", errors.New("docker-compose not found"))
	if composeErr.Status != StatusError {
		t.Errorf("expected StatusError for missing compose, got %v", composeErr.Status)
	}
}

func TestEvaluateSSH(t *testing.T) {
	// Forgejo SSH tests
	forgejoOk := EvaluateForgejoSSH("Hi alireza! You've successfully authenticated via SSH.", nil)
	if forgejoOk.Status != StatusOK {
		t.Errorf("expected StatusOK for forgejo auth, got %v", forgejoOk.Status)
	}

	forgejoDenied := EvaluateForgejoSSH("git@git.dev.manova.space: Permission denied (publickey).", errors.New("exit status 255"))
	if forgejoDenied.Status != StatusWarning {
		t.Errorf("expected StatusWarning for permission denied, got %v", forgejoDenied.Status)
	}

	forgejoRefused := EvaluateForgejoSSH("ssh: connect to host git.dev.manova.space port 22: Connection refused", errors.New("exit status 255"))
	if forgejoRefused.Status != StatusWarning {
		t.Errorf("expected StatusWarning for connection refused, got %v", forgejoRefused.Status)
	}

	// GitHub SSH tests
	ghOk := EvaluateGitHubSSH("Hi octocat! You've successfully authenticated, but GitHub does not provide shell access.", errors.New("exit status 1"))
	if ghOk.Status != StatusOK {
		t.Errorf("expected StatusOK for github auth, got %v", ghOk.Status)
	}

	ghDenied := EvaluateGitHubSSH("git@github.com: Permission denied (publickey).", errors.New("exit status 255"))
	if ghDenied.Status != StatusWarning {
		t.Errorf("expected StatusWarning for github permission denied, got %v", ghDenied.Status)
	}
}

func TestEvaluatePortScan(t *testing.T) {
	// All free
	freeScan := map[int]bool{
		10000: false,
		10050: false,
		10100: false,
	}
	resFree := EvaluatePortScan(freeScan)
	if resFree.Status != StatusOK {
		t.Errorf("expected StatusOK for free ports, got %v", resFree.Status)
	}

	// In use ports
	usedScan := map[int]bool{
		10000: false,
		10050: true,
		10100: true,
	}
	resUsed := EvaluatePortScan(usedScan)
	if resUsed.Status != StatusWarning {
		t.Errorf("expected StatusWarning for bound ports, got %v", resUsed.Status)
	}
}

func TestEvaluateOptionalTools(t *testing.T) {
	caddyOk := EvaluateCaddyVersion("v2.8.4 h1:xyz\n", nil)
	if caddyOk.Status != StatusOK {
		t.Errorf("expected StatusOK for caddy, got %v", caddyOk.Status)
	}

	caddyMissing := EvaluateCaddyVersion("", errors.New("not found"))
	if caddyMissing.Status != StatusWarning {
		t.Errorf("expected StatusWarning for missing caddy, got %v", caddyMissing.Status)
	}

	typstOk := EvaluateTypstVersion("typst 0.15.1 (unknown commit)\n", nil)
	if typstOk.Status != StatusOK {
		t.Errorf("expected StatusOK for typst, got %v", typstOk.Status)
	}

	typstMissing := EvaluateTypstVersion("", errors.New("not found"))
	if typstMissing.Status != StatusWarning {
		t.Errorf("expected StatusWarning for missing typst, got %v", typstMissing.Status)
	}
}

func TestDoctorReportAggregation(t *testing.T) {
	report := &DoctorReport{}

	if report.HasErrors() {
		t.Errorf("expected no errors for empty report")
	}
	if report.HasWarnings() {
		t.Errorf("expected no warnings for empty report")
	}

	report.Add(DiagnosticResult{
		Category: "System",
		Name:     "OS",
		Status:   StatusOK,
		Message:  "Ubuntu 26.04",
	})
	if report.HasErrors() || report.HasWarnings() {
		t.Errorf("expected only OK results")
	}

	report.Add(DiagnosticResult{
		Category: "Optional Tools",
		Name:     "Caddy",
		Status:   StatusWarning,
		Message:  "Caddy not found",
	})
	if !report.HasWarnings() {
		t.Errorf("expected HasWarnings() to be true")
	}
	if report.HasErrors() {
		t.Errorf("expected HasErrors() to be false")
	}

	report.Add(DiagnosticResult{
		Category: "Toolchain",
		Name:     "Go",
		Status:   StatusError,
		Message:  "Go not found",
	})
	if !report.HasErrors() {
		t.Errorf("expected HasErrors() to be true")
	}
	if len(report.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(report.Results))
	}
}

func TestRunDiagnostics(t *testing.T) {
	report := RunDiagnostics()
	if report == nil {
		t.Fatal("expected non-nil DoctorReport from RunDiagnostics")
	}
	if len(report.Results) == 0 {
		t.Fatal("expected diagnostic results, got 0")
	}

	categories := make(map[string]bool)
	for _, res := range report.Results {
		categories[res.Category] = true
	}

	expectedCategories := []string{"System", "Toolchain", "Runtime", "Container", "Authentication", "Ports", "Optional Tools"}
	for _, cat := range expectedCategories {
		if !categories[cat] {
			t.Errorf("expected report to contain category %q", cat)
		}
	}
}
