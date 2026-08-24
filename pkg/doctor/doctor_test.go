package doctor

import (
	"errors"
	"testing"
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

func TestParseOSRelease(t *testing.T) {
	content := `
# OS Release test file
NAME="Ubuntu"
VERSION="26.04 LTS (Resolute Raccoon)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 26.04 LTS"
VERSION_ID="26.04"
HOME_URL="https://www.ubuntu.com/"
SUPPORT_URL="https://help.ubuntu.com/"
UBUNTU_CODENAME=resolute
`
	info := ParseOSRelease(content)
	if info["ID"] != "ubuntu" {
		t.Errorf("expected ID 'ubuntu', got %q", info["ID"])
	}
	if info["VERSION_ID"] != "26.04" {
		t.Errorf("expected VERSION_ID '26.04', got %q", info["VERSION_ID"])
	}
	if info["PRETTY_NAME"] != "Ubuntu 26.04 LTS" {
		t.Errorf("expected PRETTY_NAME 'Ubuntu 26.04 LTS', got %q", info["PRETTY_NAME"])
	}
}

func TestEvaluateOS(t *testing.T) {
	tests := []struct {
		name           string
		goos           string
		osInfo         map[string]string
		expectedStatus CheckStatus
	}{
		{
			name: "Ubuntu 26.04 LTS",
			goos: "linux",
			osInfo: map[string]string{
				"ID":          "ubuntu",
				"VERSION_ID":  "26.04",
				"PRETTY_NAME": "Ubuntu 26.04 LTS",
			},
			expectedStatus: StatusOK,
		},
		{
			name: "Ubuntu 24.04 LTS",
			goos: "linux",
			osInfo: map[string]string{
				"ID":          "ubuntu",
				"VERSION_ID":  "24.04",
				"PRETTY_NAME": "Ubuntu 24.04 LTS",
			},
			expectedStatus: StatusOK,
		},
		{
			name: "Ubuntu 22.04 LTS",
			goos: "linux",
			osInfo: map[string]string{
				"ID":          "ubuntu",
				"VERSION_ID":  "22.04.4",
				"PRETTY_NAME": "Ubuntu 22.04.4 LTS",
			},
			expectedStatus: StatusOK,
		},
		{
			name: "Ubuntu Non-LTS (23.10)",
			goos: "linux",
			osInfo: map[string]string{
				"ID":          "ubuntu",
				"VERSION_ID":  "23.10",
				"PRETTY_NAME": "Ubuntu 23.10",
			},
			expectedStatus: StatusWarning,
		},
		{
			name: "Debian GNU/Linux",
			goos: "linux",
			osInfo: map[string]string{
				"ID":          "debian",
				"VERSION_ID":  "12",
				"PRETTY_NAME": "Debian GNU/Linux 12 (bookworm)",
			},
			expectedStatus: StatusWarning,
		},
		{
			name: "Fedora Linux",
			goos: "linux",
			osInfo: map[string]string{
				"ID":          "fedora",
				"VERSION_ID":  "40",
				"PRETTY_NAME": "Fedora Linux 40",
			},
			expectedStatus: StatusWarning,
		},
		{
			name:           "macOS Darwin",
			goos:           "darwin",
			osInfo:         nil,
			expectedStatus: StatusWarning,
		},
		{
			name:           "Windows",
			goos:           "windows",
			osInfo:         nil,
			expectedStatus: StatusWarning,
		},
		{
			name:           "Empty Linux os-release",
			goos:           "linux",
			osInfo:         map[string]string{},
			expectedStatus: StatusWarning,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := EvaluateOS(tc.osInfo, tc.goos)
			if res.Status != tc.expectedStatus {
				t.Errorf("expected status %v, got %v (message: %s)", tc.expectedStatus, res.Status, res.Message)
			}
		})
	}
}

func TestEvaluateGoVersion(t *testing.T) {
	// 1. Success case >= 1.23
	res := EvaluateGoVersion("go version go1.26.0 linux/amd64", nil)
	if res.Status != StatusOK {
		t.Errorf("expected StatusOK for go1.26.0, got %v", res.Status)
	}

	// 2. Outdated case < 1.23
	resOutdated := EvaluateGoVersion("go version go1.22.2 linux/amd64", nil)
	if resOutdated.Status != StatusError {
		t.Errorf("expected StatusError for go1.22.2, got %v", resOutdated.Status)
	}

	// 3. Exec error case
	resErr := EvaluateGoVersion("", errors.New("exec: not found"))
	if resErr.Status != StatusError {
		t.Errorf("expected StatusError for missing binary, got %v", resErr.Status)
	}

	// 4. Unparseable output
	resUnparseable := EvaluateGoVersion("custom go build without semver", nil)
	if resUnparseable.Status != StatusWarning {
		t.Errorf("expected StatusWarning for unparseable output, got %v", resUnparseable.Status)
	}
}

func TestEvaluateNodeAndBunVersions(t *testing.T) {
	// Node tests
	nodeOk := EvaluateNodeVersion("v24.18.0\n", nil)
	if nodeOk.Status != StatusOK {
		t.Errorf("expected StatusOK for Node v24.18.0, got %v", nodeOk.Status)
	}

	node20 := EvaluateNodeVersion("v20.10.0\n", nil)
	if node20.Status != StatusOK {
		t.Errorf("expected StatusOK for Node v20.10.0, got %v", node20.Status)
	}

	nodeOld := EvaluateNodeVersion("v18.19.0\n", nil)
	if nodeOld.Status != StatusError {
		t.Errorf("expected StatusError for Node v18.19.0, got %v", nodeOld.Status)
	}

	nodeMissing := EvaluateNodeVersion("", errors.New("not found"))
	if nodeMissing.Status != StatusError {
		t.Errorf("expected StatusError for missing node, got %v", nodeMissing.Status)
	}

	// Bun tests
	bunOk := EvaluateBunVersion("1.3.14\n", nil)
	if bunOk.Status != StatusOK {
		t.Errorf("expected StatusOK for Bun 1.3.14, got %v", bunOk.Status)
	}

	bun11 := EvaluateBunVersion("1.1.0\n", nil)
	if bun11.Status != StatusOK {
		t.Errorf("expected StatusOK for Bun 1.1.0, got %v", bun11.Status)
	}

	bunOld := EvaluateBunVersion("0.8.0\n", nil)
	if bunOld.Status != StatusError {
		t.Errorf("expected StatusError for Bun 0.8.0, got %v", bunOld.Status)
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

func TestEvaluateZshAndOhMyZsh(t *testing.T) {
	// Zsh success
	res := EvaluateZsh("zsh 5.9 (x86_64-ubuntu-linux-gnu)", nil)
	if res.Status != StatusOK {
		t.Errorf("expected StatusOK for valid zsh version, got %s", res.Status)
	}

	// Zsh missing
	resErr := EvaluateZsh("", errors.New("executable file not found in $PATH"))
	if resErr.Status != StatusError {
		t.Errorf("expected StatusError for missing zsh, got %s", resErr.Status)
	}

	// Oh My Zsh present
	omzRes := EvaluateOhMyZsh(true)
	if omzRes.Status != StatusOK {
		t.Errorf("expected StatusOK for present OMZ, got %s", omzRes.Status)
	}

	// Oh My Zsh missing
	omzResMissing := EvaluateOhMyZsh(false)
	if omzResMissing.Status != StatusError {
		t.Errorf("expected StatusError for missing OMZ, got %s", omzResMissing.Status)
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

	expectedCategories := []string{"System", "Shell", "Toolchain", "Runtime", "Container", "Authentication", "Ports", "Optional Tools"}
	for _, cat := range expectedCategories {
		if !categories[cat] {
			t.Errorf("expected report to contain category %q", cat)
		}
	}
}
