package domain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/host"
)

// EvaluateHost turns a host readiness report into a diagnostic result.
func EvaluateHost(r host.Report) DiagnosticResult {
	if r.OK {
		return DiagnosticResult{
			Category: "System",
			Name:     "Host",
			Status:   StatusOK,
			Message:  "Supported host: Ubuntu 24.04/26.04 LTS amd64, zsh, ~/.local/bin",
		}
	}
	return DiagnosticResult{
		Category:      "System",
		Name:          "Host",
		Status:        StatusError,
		Message:       strings.TrimSpace(host.Format(r)),
		FixSuggestion: "Use Ubuntu 24.04 or 26.04 LTS (amd64, WSL2 or native), chsh to zsh, and put ~/.local/bin on PATH.",
	}
}

// EvaluateGitVersion evaluates the output of `git version`.
func EvaluateGitVersion(rawOutput string, execErr error) DiagnosticResult {
	category := "Toolchain"
	name := "Git"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       "Git CLI not found in PATH",
			FixSuggestion: "Install Git: sudo apt install git or visit https://git-scm.com.",
		}
	}

	v, err := ParseSemver(rawOutput, "git")
	if err != nil {
		return DiagnosticResult{
			Category: category,
			Name:     name,
			Status:   StatusOK,
			Message:  strings.TrimSpace(rawOutput),
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  fmt.Sprintf("Git v%s installed", v),
	}
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
			FixSuggestion: "Install Go >= 1.26.0: sudo apt install golang-go or visit https://go.dev/dl/",
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

	if CompareVersions(v, "1.26.0") < 0 {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       fmt.Sprintf("Go v%s is below required version (>= 1.26.0)", v),
			FixSuggestion: "Upgrade Go to >= 1.26.0: visit https://go.dev/dl/ or use mise/asdf.",
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  fmt.Sprintf("Go v%s installed (>= 1.26.0 required)", v),
	}
}

// EvaluateNodeVersion evaluates the output of `node -v`.
// Passes if Node >= 24.0.0 (ADR-023: Next.js standalone runners and tools require Node 24).
func EvaluateNodeVersion(rawOutput string, execErr error) DiagnosticResult {
	category := "Runtime"
	name := "Node.js"

	if execErr != nil {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       "Node.js not found in PATH",
			FixSuggestion: "Install Node.js 24 LTS: https://nodejs.org or via fnm/nvm (fnm install 24).",
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

	if CompareVersions(v, "24.0.0") < 0 {
		return DiagnosticResult{
			Category:      category,
			Name:          name,
			Status:        StatusError,
			Message:       fmt.Sprintf("Node.js v%s is below required version (>= 24)", v),
			FixSuggestion: "Install Node.js 24 LTS: fnm install 24 or nvm install 24.",
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  fmt.Sprintf("Node.js v%s installed (>= 24 required)", v),
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

	return DiagnosticResult{Category: category, Name: name, Status: StatusOK, Message: fmt.Sprintf("Bun v%s installed; compatibility requires project manifest validation", v)}
}

// EvaluateDockerDaemon evaluates the output of `docker info`. cliInstalled
// reports whether the docker binary exists on PATH, which changes the
// remediation text.
func EvaluateDockerDaemon(rawOutput string, execErr error, cliInstalled bool) DiagnosticResult {
	category := "Container"
	name := "Docker Daemon"

	if execErr != nil {
		if !cliInstalled {
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

// EvaluatePortScan evaluates port scan results for ports in 10000-10300.
func EvaluatePortScan(scanResults map[int]bool) DiagnosticResult {
	category := "Ports"
	name := "Dev Port Range (10000-10300)"

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
			Message:       fmt.Sprintf("Found %d bound port(s) in range 10000-10300: %v", len(inUse), inUse),
			FixSuggestion: "Stop conflicting processes or run 'manova dev down' to free dev ports.",
		}
	}

	return DiagnosticResult{
		Category: category,
		Name:     name,
		Status:   StatusOK,
		Message:  "All dev ports in range 10000-10300 are free",
	}
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

	return DiagnosticResult{Category: category, Name: name, Status: StatusOK, Message: "Caddy installed"}
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

	return DiagnosticResult{Category: category, Name: name, Status: StatusOK, Message: "Typst installed"}
}
