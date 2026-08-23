package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/manovaspace/orbit-cli/pkg/ports"
)

// Default timeouts for diagnostic command executions.
const (
	defaultCommandTimeout = 5 * time.Second
	shortCommandTimeout   = 3 * time.Second
)

// RunDiagnostics executes the full suite of pre-flight system diagnostics and returns an aggregated DoctorReport.
func RunDiagnostics() *DoctorReport {
	report := &DoctorReport{
		Results: make([]DiagnosticResult, 0),
	}

	report.Add(CheckOS())
	report.Add(CheckGo())
	report.AddAll(CheckNodeAndBun())
	report.AddAll(CheckDocker())
	report.AddAll(CheckSSHAuth())
	report.Add(CheckPorts())
	report.AddAll(CheckOptionalTools())

	return report
}

// CheckOS inspects the operating system release file (/etc/os-release) to verify compatibility with Ubuntu LTS releases.
func CheckOS() DiagnosticResult {
	if runtime.GOOS != "linux" {
		return EvaluateOS(nil, runtime.GOOS)
	}

	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		// Fallback to /usr/lib/os-release
		content, err = os.ReadFile("/usr/lib/os-release")
		if err != nil {
			return EvaluateOS(nil, runtime.GOOS)
		}
	}

	osInfo := ParseOSRelease(string(content))
	return EvaluateOS(osInfo, runtime.GOOS)
}

// ParseOSRelease parses the key-value pairs from standard Linux os-release format.
func ParseOSRelease(content string) map[string]string {
	data := make(map[string]string)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")
		data[key] = val
	}
	return data
}

// EvaluateOS checks parsed OS info against supported Ubuntu LTS versions.
func EvaluateOS(osInfo map[string]string, goos string) DiagnosticResult {
	category := "System"
	name := "Operating System"

	if goos != "linux" {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       fmt.Sprintf("Non-Linux OS detected (%s). Manova CLI is optimized for Linux (Ubuntu 22.04+).", goos),
			FixSuggestion: "Run inside WSL2 or a Linux container for full development stack support.",
		}
	}

	if len(osInfo) == 0 {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       "Linux distribution could not be identified (/etc/os-release unavailable)",
			FixSuggestion: "Ensure system has a standard /etc/os-release file.",
		}
	}

	id := strings.ToLower(osInfo["ID"])
	versionID := strings.TrimSpace(osInfo["VERSION_ID"])
	prettyName := osInfo["PRETTY_NAME"]
	if prettyName == "" {
		prettyName = osInfo["NAME"]
	}
	if prettyName == "" {
		prettyName = "Linux"
	}

	if id == "ubuntu" {
		if strings.HasPrefix(versionID, "26.04") ||
			strings.HasPrefix(versionID, "24.04") ||
			strings.HasPrefix(versionID, "22.04") {
			return DiagnosticResult{
				Category: category,
				Name:     name,
				Status:   StatusOK,
				Message:  fmt.Sprintf("Supported OS: %s", prettyName),
			}
		}

		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       fmt.Sprintf("Ubuntu %s detected (recommended: 22.04, 24.04, or 26.04 LTS)", versionID),
			FixSuggestion: "Consider running on an LTS release: Ubuntu 22.04, 24.04, or 26.04 LTS.",
		}
	}

	return DiagnosticResult{
		Category:      category,
		Name:          name,
		Status:        StatusWarning,
		Message:       fmt.Sprintf("Non-Ubuntu Linux distribution detected (%s); Manova officially supports Ubuntu 22.04/24.04/26.04 LTS", prettyName),
		FixSuggestion: "Verify required dependencies manually or use Ubuntu in WSL2/Docker.",
	}
}

// CheckGo checks if Go compiler is installed and meets the version requirement (>= 1.23).
func CheckGo() DiagnosticResult {
	out, err := runCommand(defaultCommandTimeout, "go", "version")
	return EvaluateGoVersion(out, err)
}

// EvaluateGoVersion evaluates the output of `go version`.
func EvaluateGoVersion(rawOutput string, execErr error) DiagnosticResult {
	category := "Toolchain"
	name := "Go Compiler"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       "Go compiler not found in PATH",
			FixSuggestion: "Install Go >= 1.23: sudo apt install golang-go or visit https://go.dev/dl/",
		}
	}

	v, err := ParseSemver(rawOutput, "go")
	if err != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       fmt.Sprintf("Unable to parse Go version: %s", strings.TrimSpace(rawOutput)),
			FixSuggestion: "Verify Go installation with 'go version'.",
		}
	}

	if CompareVersions(v, "1.23") < 0 {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       fmt.Sprintf("Go v%s is below required version (>= 1.23)", v),
			FixSuggestion: "Upgrade Go to >= 1.23: visit https://go.dev/dl/ or use mise/asdf.",
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  fmt.Sprintf("Go v%s installed (>= 1.23 required)", v),
	}
}

// CheckNodeAndBun checks Node.js (>= 20/22) and Bun (>= 1.1).
func CheckNodeAndBun() []DiagnosticResult {
	var results []DiagnosticResult

	// Node check
	nodeOut, nodeErr := runCommand(defaultCommandTimeout, "node", "-v")
	results = append(results, EvaluateNodeVersion(nodeOut, nodeErr))

	// Bun check
	bunOut, bunErr := runCommand(defaultCommandTimeout, "bun", "-v")
	results = append(results, EvaluateBunVersion(bunOut, bunErr))

	return results
}

// EvaluateNodeVersion evaluates the output of `node -v`.
func EvaluateNodeVersion(rawOutput string, execErr error) DiagnosticResult {
	category := "Runtime"
	name := "Node.js"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       "Node.js not found in PATH",
			FixSuggestion: "Install Node.js 22 LTS: https://nodejs.org or via fnm/nvm.",
		}
	}

	v, err := ParseSemver(rawOutput, "node")
	if err != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       fmt.Sprintf("Unable to parse Node.js version: %s", strings.TrimSpace(rawOutput)),
			FixSuggestion: "Verify Node.js installation with 'node -v'.",
		}
	}

	if CompareVersions(v, "20.0.0") < 0 {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       fmt.Sprintf("Node.js v%s is below required version (>= 20, recommended 22+ LTS)", v),
			FixSuggestion: "Install Node.js 22 LTS: fnm install 22 or nvm install 22.",
		}
	}

	if CompareVersions(v, "22.0.0") < 0 {
		return DiagnosticResult{
			Category: category,
			Name:     name,
			Status:   StatusOK,
			Message:  fmt.Sprintf("Node.js v%s installed (v22+ LTS recommended)", v),
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  fmt.Sprintf("Node.js v%s installed (>= 22 LTS)", v),
	}
}

// EvaluateBunVersion evaluates the output of `bun -v`.
func EvaluateBunVersion(rawOutput string, execErr error) DiagnosticResult {
	category := "Runtime"
	name := "Bun"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       "Bun JavaScript runtime not found in PATH",
			FixSuggestion: "Install Bun: curl -fsSL https://bun.sh/install | bash",
		}
	}

	v, err := ParseSemver(rawOutput, "bun")
	if err != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       fmt.Sprintf("Unable to parse Bun version: %s", strings.TrimSpace(rawOutput)),
			FixSuggestion: "Verify Bun installation with 'bun -v'.",
		}
	}

	if CompareVersions(v, "1.1.0") < 0 {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       fmt.Sprintf("Bun v%s is below required version (>= 1.1)", v),
			FixSuggestion: "Upgrade Bun: bun upgrade",
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  fmt.Sprintf("Bun v%s installed (>= 1.1 required)", v),
	}
}

// CheckDocker checks if Docker daemon is running and Docker Compose v2 is available.
func CheckDocker() []DiagnosticResult {
	var results []DiagnosticResult

	// Docker daemon check
	infoOut, infoErr := runCommand(defaultCommandTimeout, "docker", "info")
	results = append(results, EvaluateDockerDaemon(infoOut, infoErr))

	// Docker Compose check
	composeOut, composeErr := runCommand(defaultCommandTimeout, "docker", "compose", "version")
	if composeErr != nil {
		// Fallback to docker-compose
		composeOut, composeErr = runCommand(defaultCommandTimeout, "docker-compose", "version")
	}
	results = append(results, EvaluateDockerCompose(composeOut, composeErr))

	return results
}

// EvaluateDockerDaemon evaluates the output of `docker info`.
func EvaluateDockerDaemon(rawOutput string, execErr error) DiagnosticResult {
	category := "Container"
	name := "Docker Daemon"

	if execErr != nil {
		if _, pathErr := exec.LookPath("docker"); pathErr != nil {
			return DiagnosticResult{
				Category:      category,
				Name:          name,
				Status:        StatusError,
				Message:       "Docker CLI not found in PATH",
				FixSuggestion: "Install Docker Engine: https://docs.docker.com/engine/install/ubuntu/.",
			}
		}

		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       "Docker daemon is not running or socket is inaccessible",
			FixSuggestion: "Start Docker daemon: sudo systemctl start docker (and ensure user is in 'docker' group).",
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  "Docker daemon is running and responsive",
	}
}

// EvaluateDockerCompose evaluates the output of `docker compose version`.
func EvaluateDockerCompose(rawOutput string, execErr error) DiagnosticResult {
	category := "Container"
	name := "Docker Compose"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       "Docker Compose v2 not found",
			FixSuggestion: "Install Docker Compose plugin: sudo apt install docker-compose-plugin.",
		}
	}

	v, err := ParseSemver(rawOutput, "docker-compose")
	if err == nil && v != "" {
		return DiagnosticResult{
			Category: category,
			Name:     name,
			Status:   StatusOK,
			Message:  fmt.Sprintf("Docker Compose v%s available", v),
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  "Docker Compose v2 available",
	}
}

// CheckSSHAuth checks SSH agent status and connectivity to git.dev.manova.space and github.com.
func CheckSSHAuth() []DiagnosticResult {
	var results []DiagnosticResult

	// 1. SSH Agent check
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock == "" {
		results = append(results, DiagnosticResult{
			Category:      "Authentication",
			Name:          "SSH Agent",
			Status:        StatusWarning,
			Message:       "SSH agent is not running (SSH_AUTH_SOCK unset)",
			FixSuggestion: "Start SSH agent: eval $(ssh-agent -s) && ssh-add.",
		})
	} else {
		addOut, addErr := runCommand(shortCommandTimeout, "ssh-add", "-l")
		if addErr == nil {
			results = append(results, DiagnosticResult{
				Category: "Authentication",
				Name:     "SSH Agent",
				Status:   StatusOK,
				Message:  "SSH agent active with loaded identities",
			})
		} else if strings.Contains(addOut, "The agent has no identities") {
			results = append(results, DiagnosticResult{
				Category:      "Authentication",
				Name:          "SSH Agent",
				Status:        StatusWarning,
				Message:       "SSH agent running but no keys loaded",
				FixSuggestion: "Load your SSH key: ssh-add ~/.ssh/id_ed25519.",
			})
		} else {
			results = append(results, DiagnosticResult{
				Category: "Authentication",
				Name:     "SSH Agent",
				Status:   StatusOK,
				Message:  "SSH agent socket connected",
			})
		}
	}

	// 2. Forgejo SSH check
	forgejoOut, forgejoErr := runCommand(shortCommandTimeout, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "-o", "StrictHostKeyChecking=accept-new", "-T", "git@git.dev.manova.space")
	results = append(results, EvaluateForgejoSSH(forgejoOut, forgejoErr))

	// 3. GitHub SSH check
	githubOut, githubErr := runCommand(shortCommandTimeout, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "-o", "StrictHostKeyChecking=accept-new", "-T", "git@github.com")
	results = append(results, EvaluateGitHubSSH(githubOut, githubErr))

	return results
}

// EvaluateForgejoSSH evaluates SSH probe output for git.dev.manova.space.
func EvaluateForgejoSSH(rawOutput string, execErr error) DiagnosticResult {
	category := "Authentication"
	name := "Forgejo SSH (git.dev.manova.space)"

	combined := rawOutput
	if execErr != nil {
		combined += " " + execErr.Error()
	}

	if strings.Contains(combined, "successfully authenticated") ||
		strings.Contains(combined, "Hi ") ||
		strings.Contains(combined, "PTY allocation request failed") ||
		strings.Contains(combined, "Forgejo") ||
		strings.Contains(combined, "Gitea") {
		return DiagnosticResult{
			Category: category,
			Name:     name,
			Status:   StatusOK,
			Message:  "SSH connection to git.dev.manova.space successful",
		}
	}

	if strings.Contains(combined, "Permission denied") {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       "SSH authentication to git.dev.manova.space failed (Permission denied)",
			FixSuggestion: "Ensure your public key is added to your Forgejo profile and loaded into ssh-agent.",
		}
	}

	if strings.Contains(combined, "Connection refused") ||
		strings.Contains(combined, "Could not resolve") ||
		strings.Contains(combined, "timed out") ||
		strings.Contains(combined, "No route to host") {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       "Could not reach git.dev.manova.space (stack or host may be offline)",
			FixSuggestion: "Start local dev stack with 'manova dev up' or check network/DNS configuration.",
		}
	}

	return DiagnosticResult{
		Category:      category,
		Name:          name,
		Status:        StatusWarning,
		Message:       fmt.Sprintf("Forgejo SSH probe: %s", strings.TrimSpace(rawOutput)),
		FixSuggestion: "Verify SSH key and network connectivity to git.dev.manova.space.",
	}
}

// EvaluateGitHubSSH evaluates SSH probe output for github.com.
func EvaluateGitHubSSH(rawOutput string, execErr error) DiagnosticResult {
	category := "Authentication"
	name := "GitHub SSH (github.com)"

	combined := rawOutput
	if execErr != nil {
		combined += " " + execErr.Error()
	}

	if strings.Contains(combined, "successfully authenticated") ||
		strings.Contains(combined, "Hi ") {
		return DiagnosticResult{
			Category: category,
			Name:     name,
			Status:   StatusOK,
			Message:  "SSH authentication to github.com successful",
		}
	}

	if strings.Contains(combined, "Permission denied") {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       "SSH authentication to github.com failed (Permission denied)",
			FixSuggestion: "Add your SSH key to GitHub: https://github.com/settings/keys.",
		}
	}

	if strings.Contains(combined, "timed out") ||
		strings.Contains(combined, "Could not resolve") ||
		strings.Contains(combined, "Connection refused") {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       "Could not connect to github.com SSH",
			FixSuggestion: "Check internet connectivity and firewall settings.",
		}
	}

	return DiagnosticResult{
		Category:      category,
		Name:          name,
		Status:        StatusWarning,
		Message:       fmt.Sprintf("GitHub SSH probe: %s", strings.TrimSpace(rawOutput)),
		FixSuggestion: "Verify GitHub SSH configuration.",
	}
}

// CheckPorts scans the standard dev port range (10000-10250) for unexpected bindings.
func CheckPorts() DiagnosticResult {
	scanResults := ports.ScanRange(10000, 10250)
	return EvaluatePortScan(scanResults)
}

// EvaluatePortScan evaluates port scan results for ports in 10000-10250.
func EvaluatePortScan(scanResults map[int]bool) DiagnosticResult {
	category := "Ports"
	name := "Dev Port Range (10000-10250)"

	var inUse []int
	for port, used := range scanResults {
		if used {
			inUse = append(inUse, port)
		}
	}
	sort.Ints(inUse)

	if len(inUse) > 0 {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       fmt.Sprintf("Found %d bound port(s) in range 10000-10250: %v", len(inUse), inUse),
			FixSuggestion: "Stop conflicting processes or run 'manova dev down' to free dev ports.",
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  "All dev ports in range 10000-10250 are free",
	}
}

// CheckOptionalTools checks for presence of Caddy and Typst.
func CheckOptionalTools() []DiagnosticResult {
	var results []DiagnosticResult

	// Caddy check
	caddyOut, caddyErr := runCommand(shortCommandTimeout, "caddy", "version")
	results = append(results, EvaluateCaddyVersion(caddyOut, caddyErr))

	// Typst check
	typstOut, typstErr := runCommand(shortCommandTimeout, "typst", "--version")
	results = append(results, EvaluateTypstVersion(typstOut, typstErr))

	return results
}

// EvaluateCaddyVersion evaluates the output of `caddy version`.
func EvaluateCaddyVersion(rawOutput string, execErr error) DiagnosticResult {
	category := "Optional Tools"
	name := "Caddy Reverse Proxy"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       "Caddy reverse proxy not found in PATH (optional for local ingress)",
			FixSuggestion: "Install Caddy: sudo apt install caddy or visit https://caddyserver.com.",
		}
	}

	v, err := ParseSemver(rawOutput, "caddy")
	if err == nil && v != "" {
		return DiagnosticResult{
			Category: category,
			Name:     name,
			Status:   StatusOK,
			Message:  fmt.Sprintf("Caddy v%s installed", v),
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  "Caddy installed",
	}
}

// EvaluateTypstVersion evaluates the output of `typst --version`.
func EvaluateTypstVersion(rawOutput string, execErr error) DiagnosticResult {
	category := "Optional Tools"
	name := "Typst Compiler"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusWarning,
			Message:       "Typst compiler not found in PATH (optional, for documentation rendering)",
			FixSuggestion: "Install Typst: cargo install typst-cli or visit https://github.com/typst/typst/releases.",
		}
	}

	v, err := ParseSemver(rawOutput, "typst")
	if err == nil && v != "" {
		return DiagnosticResult{
			Category: category,
			Name:     name,
			Status:   StatusOK,
			Message:  fmt.Sprintf("Typst v%s installed", v),
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  "Typst installed",
	}
}

// ParseSemver parses semantic version from tool version outputs.
func ParseSemver(raw string, tool string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty version string")
	}

	var pattern *regexp.Regexp
	switch strings.ToLower(tool) {
	case "go":
		pattern = regexp.MustCompile(`go([0-9]+(?:\.[0-9]+)*)`)
	case "typst":
		pattern = regexp.MustCompile(`(?:typst\s+)?v?([0-9]+(?:\.[0-9]+)*)`)
	case "docker-compose":
		pattern = regexp.MustCompile(`(?:version\s+)?v?([0-9]+(?:\.[0-9]+)*)`)
	default:
		pattern = regexp.MustCompile(`v?([0-9]+(?:\.[0-9]+)*)`)
	}

	matches := pattern.FindStringSubmatch(raw)
	if len(matches) > 1 && matches[1] != "" {
		return matches[1], nil
	}

	// Fallback to generic number pattern
	generic := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)+)`)
	matches = generic.FindStringSubmatch(raw)
	if len(matches) > 1 && matches[1] != "" {
		return matches[1], nil
	}

	return "", fmt.Errorf("unable to parse %s version from %q", tool, raw)
}

// parseSemver is an alias for ParseSemver for backward/internal compatibility.
func parseSemver(raw string, tool string) (string, error) {
	return ParseSemver(raw, tool)
}

// CompareVersions compares two semver strings (e.g. "1.23.1", "1.23", "22.8.0").
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2.
func CompareVersions(v1, v2 string) int {
	p1 := parseVersionComponents(v1)
	p2 := parseVersionComponents(v2)

	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}

	for i := 0; i < maxLen; i++ {
		n1 := 0
		if i < len(p1) {
			n1 = p1[i]
		}
		n2 := 0
		if i < len(p2) {
			n2 = p2[i]
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	return 0
}

func parseVersionComponents(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.TrimPrefix(v, "go")
	parts := strings.Split(v, ".")
	var res []int
	for _, p := range parts {
		var numStr string
		for _, r := range p {
			if unicode.IsDigit(r) {
				numStr += string(r)
			} else {
				break
			}
		}
		if numStr == "" {
			break
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			break
		}
		res = append(res, n)
	}
	return res
}

func runCommand(timeout time.Duration, name string, args ...string) (string, error) {
	return runCommandWithEnv(timeout, nil, name, args...)
}

func runCommandWithEnv(timeout time.Duration, extraEnv []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	out, err := cmd.CombinedOutput()
	return string(out), err
}
