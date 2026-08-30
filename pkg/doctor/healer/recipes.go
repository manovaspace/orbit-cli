package healer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
)

// ensureRunner returns the provided runner or DefaultRunner if nil.
func ensureRunner(r Runner) Runner {
	if r != nil {
		return r
	}
	return &DefaultRunner{}
}

// defaultIsRoot returns true if running as root user.
func defaultIsRoot() bool {
	return os.Geteuid() == 0
}

// checkElevation checks whether the process has root rights or sudo command available.
func checkElevation(isRoot bool) error {
	if isRoot {
		return nil
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("root privileges or 'sudo' command required to install system packages")
	}
	return nil
}

// sudoPrefix returns "sudo " when running as non-root user, or empty string if root.
func sudoPrefix(isRoot bool) string {
	if isRoot {
		return ""
	}
	return "sudo "
}

// ============================================================================
// GoHealer
// ============================================================================

// GoHealer downloads and installs the official Go compiler release into /usr/local/go.
type GoHealer struct {
	Runner  Runner
	Version string
	GOOS    string
	GOARCH  string
	IsRoot  func() bool
}

// NewGoHealer creates a new GoHealer instance with default parameters (Go 1.26.4).
func NewGoHealer() *GoHealer {
	return &GoHealer{
		Version: "1.26.4",
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
		IsRoot:  defaultIsRoot,
	}
}

// Name returns the identifier of the healer.
func (h *GoHealer) Name() string {
	return "Go 1.26"
}

// CanHeal checks whether the result represents a missing or outdated Go installation.
func (h *GoHealer) CanHeal(result doctor.DiagnosticResult) bool {
	if result.Status == doctor.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	if name == "go compiler" || name == "go" {
		return true
	}
	if result.Category == "Toolchain" && strings.Contains(name, "go") {
		return true
	}
	if strings.Contains(strings.ToLower(result.Message), "go compiler") {
		return true
	}
	return false
}

// Heal installs Go 1.26 to /usr/local/go and configures environment PATH.
func (h *GoHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	runner := ensureRunner(h.Runner)
	goos := h.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "linux" {
		return fmt.Errorf("Go auto-healing is supported on Linux only (detected: %s)", goos)
	}

	goarch := h.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if goarch != "amd64" {
		return fmt.Errorf("unsupported architecture for Go auto-healing: %s (supported: amd64)", goarch)
	}

	version := h.Version
	if version == "" {
		version = "1.26.4"
	}

	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}

	if err := checkElevation(isRootUser); err != nil {
		return err
	}

	sp := sudoPrefix(isRootUser)
	tarballName := fmt.Sprintf("go%s.%s-%s.tar.gz", version, goos, goarch)
	downloadURL := fmt.Sprintf("https://go.dev/dl/%s", tarballName)

	progress(fmt.Sprintf("Downloading Go v%s (%s-%s)...", version, goos, goarch))

	script := fmt.Sprintf(`set -euo pipefail
TMP_DIR=$(mktemp -d)
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT
curl -fsSL "%s" -o "$TMP_DIR/%s"
%srm -rf /usr/local/go
%star -C /usr/local -xzf "$TMP_DIR/%s"
%smkdir -p /etc/profile.d
echo 'export PATH=$PATH:/usr/local/go/bin' | %stee /etc/profile.d/orbit-go.sh > /dev/null
%schmod 644 /etc/profile.d/orbit-go.sh
`, downloadURL, tarballName, sp, sp, tarballName, sp, sp, sp)

	progress("Extracting Go to /usr/local/go and configuring /etc/profile.d/orbit-go.sh...")
	if out, err := runner.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Go %s: %w\nOutput: %s", version, err, strings.TrimSpace(out))
	}

	// Update PATH in current process environment
	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, "/usr/local/go/bin") {
		_ = os.Setenv("PATH", currentPath+":/usr/local/go/bin")
	}

	progress(fmt.Sprintf("Go v%s installed successfully", version))
	return nil
}

// ============================================================================
// BunHealer
// ============================================================================

// BunHealer executes the official Bun installation script.
type BunHealer struct {
	Runner Runner
}

// NewBunHealer creates a new BunHealer instance.
func NewBunHealer() *BunHealer {
	return &BunHealer{}
}

// Name returns the identifier of the healer.
func (h *BunHealer) Name() string {
	return "Bun"
}

// CanHeal checks whether the result represents a missing or outdated Bun installation.
func (h *BunHealer) CanHeal(result doctor.DiagnosticResult) bool {
	if result.Status == doctor.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	return name == "bun" || strings.Contains(name, "bun javascript") || strings.Contains(name, "bun runtime")
}

// Heal runs the official Bun installer script (curl -fsSL https://bun.sh/install | bash).
func (h *BunHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	runner := ensureRunner(h.Runner)
	progress("Downloading and running Bun installer...")

	script := `set -euo pipefail
if ! command -v unzip >/dev/null 2>&1; then
	if command -v apt-get >/dev/null 2>&1; then
		export DEBIAN_FRONTEND=noninteractive
		if [ "$(id -u)" -eq 0 ]; then
			apt-get update -y && apt-get install -y unzip
		elif command -v sudo >/dev/null 2>&1; then
			sudo apt-get update -y && sudo apt-get install -y unzip
		fi
	fi
fi
curl -fsSL https://bun.sh/install | bash
`

	if out, err := runner.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Bun: %w\nOutput: %s", err, strings.TrimSpace(out))
	}

	// Add ~/.bun/bin to current process PATH
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		if usr, err := user.Current(); err == nil {
			homeDir = usr.HomeDir
		}
	}
	if homeDir != "" {
		bunBin := filepath.Join(homeDir, ".bun", "bin")
		currentPath := os.Getenv("PATH")
		if !strings.Contains(currentPath, bunBin) {
			_ = os.Setenv("PATH", bunBin+":"+currentPath)
		}
	}

	progress("Bun installed successfully")
	return nil
}

// ============================================================================
// NodeHealer
// ============================================================================

// NodeHealer installs Node.js 24 LTS via NodeSource package repository.
type NodeHealer struct {
	Runner Runner
	IsRoot func() bool
}

// NewNodeHealer creates a new NodeHealer instance.
func NewNodeHealer() *NodeHealer {
	return &NodeHealer{
		IsRoot: defaultIsRoot,
	}
}

// Name returns the identifier of the healer.
func (h *NodeHealer) Name() string {
	return "Node.js 24 LTS"
}

// CanHeal checks whether the result represents a missing or outdated Node.js installation.
func (h *NodeHealer) CanHeal(result doctor.DiagnosticResult) bool {
	if result.Status == doctor.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	return name == "node.js" || name == "nodejs" || name == "node" || strings.Contains(name, "node.js")
}

// Heal installs Node.js 24 LTS via official NodeSource repository.
func (h *NodeHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	runner := ensureRunner(h.Runner)
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}

	if err := checkElevation(isRootUser); err != nil {
		return err
	}

	sp := sudoPrefix(isRootUser)
	sudoDashE := ""
	if !isRootUser {
		sudoDashE = "sudo -E "
	}

	progress("Setting up NodeSource repository for Node.js 24 LTS...")

	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
curl -fsSL https://deb.nodesource.com/setup_24.x | %sbash -
%sapt-get update -y
%sapt-get install -y nodejs
`, sudoDashE, sp, sp)

	progress("Installing nodejs package...")
	if out, err := runner.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Node.js 24: %w\nOutput: %s", err, strings.TrimSpace(out))
	}

	progress("Node.js 24 LTS installed successfully")
	return nil
}

// ============================================================================
// GitHealer
// ============================================================================

// GitHealer installs Git and CA certificates via system package manager.
type GitHealer struct {
	Runner Runner
	IsRoot func() bool
}

// NewGitHealer creates a new GitHealer instance.
func NewGitHealer() *GitHealer {
	return &GitHealer{
		IsRoot: defaultIsRoot,
	}
}

// Name returns the identifier of the healer.
func (h *GitHealer) Name() string {
	return "Git"
}

// CanHeal checks whether the result represents a missing Git installation.
func (h *GitHealer) CanHeal(result doctor.DiagnosticResult) bool {
	if result.Status == doctor.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	// Exclude SSH authentication checks
	if strings.Contains(name, "ssh") || strings.Contains(name, "forgejo") || strings.Contains(name, "github") {
		return false
	}
	if name == "git" || strings.Contains(name, "git cli") || strings.Contains(name, "git version") {
		return true
	}
	if strings.Contains(strings.ToLower(result.Message), "git not found") || strings.Contains(strings.ToLower(result.FixSuggestion), "install git") {
		return true
	}
	return false
}

// Heal installs git and ca-certificates using apt-get.
func (h *GitHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	runner := ensureRunner(h.Runner)
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}

	if err := checkElevation(isRootUser); err != nil {
		return err
	}

	sp := sudoPrefix(isRootUser)
	progress("Installing git and ca-certificates via apt-get...")

	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
%sapt-get update -y
%sapt-get install -y git ca-certificates
`, sp, sp)

	if out, err := runner.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install git: %w\nOutput: %s", err, strings.TrimSpace(out))
	}

	progress("Git and ca-certificates installed successfully")
	return nil
}

// ============================================================================
// DockerComposeHealer
// ============================================================================

// DockerComposeHealer installs the Docker Compose v2 plugin via apt-get.
type DockerComposeHealer struct {
	Runner Runner
	IsRoot func() bool
}

// NewDockerComposeHealer creates a new DockerComposeHealer instance.
func NewDockerComposeHealer() *DockerComposeHealer {
	return &DockerComposeHealer{
		IsRoot: defaultIsRoot,
	}
}

// Name returns the identifier of the healer.
func (h *DockerComposeHealer) Name() string {
	return "Docker Compose"
}

// CanHeal checks whether the result represents a missing Docker Compose plugin.
func (h *DockerComposeHealer) CanHeal(result doctor.DiagnosticResult) bool {
	if result.Status == doctor.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	if name == "docker compose" || strings.Contains(name, "docker compose") || strings.Contains(name, "docker-compose") {
		return true
	}
	if strings.Contains(strings.ToLower(result.FixSuggestion), "docker-compose-plugin") {
		return true
	}
	return false
}

// Heal installs docker-compose-plugin using apt-get.
func (h *DockerComposeHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	runner := ensureRunner(h.Runner)
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}

	if err := checkElevation(isRootUser); err != nil {
		return err
	}

	sp := sudoPrefix(isRootUser)
	progress("Installing docker-compose-plugin via apt-get...")

	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
%sapt-get update -y
%sapt-get install -y docker-compose-plugin 2>/dev/null || %sapt-get install -y docker-compose-v2
`, sp, sp, sp)

	if out, err := runner.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install docker-compose-plugin: %w\nOutput: %s", err, strings.TrimSpace(out))
	}

	progress("Docker Compose plugin installed successfully")
	return nil
}
