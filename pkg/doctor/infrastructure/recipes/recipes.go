// Package recipes implements the doctor auto-healing recipes over runner.Runner.
package recipes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Shell runs remediation scripts and resolves programs on PATH.
type Shell interface {
	RunShell(ctx context.Context, script string) (string, error)
	LookPath(name string) (string, error)
}

// RunnerShell runs scripts with bash through a runner.Runner.
type RunnerShell struct {
	Runner runner.Runner
}

// RunShell executes script with bash -c and returns its combined output.
func (s RunnerShell) RunShell(ctx context.Context, script string) (string, error) {
	res, err := s.Runner.Run(ctx, "", "bash", "-c", script)
	return res.Stdout + res.Stderr, err
}

// LookPath resolves a program on PATH.
func (s RunnerShell) LookPath(name string) (string, error) { return exec.LookPath(name) }

// NewDefaultRegistry builds the nine standard Orbit healers over r.
func NewDefaultRegistry(r runner.Runner) *domain.HealerRegistry {
	shell := RunnerShell{Runner: r}
	reg := domain.NewHealerRegistry()
	reg.Register(NewZshHealer(shell))
	reg.Register(NewGitHealer(shell))
	reg.Register(NewGoHealer(shell))
	reg.Register(NewBunHealer(shell))
	reg.Register(NewNodeHealer(shell))
	reg.Register(NewDockerDaemonHealer(shell))
	reg.Register(NewDockerComposeHealer(shell))
	reg.Register(NewCaddyHealer(shell))
	reg.Register(NewTypstHealer(shell))
	return reg
}

// defaultIsRoot returns true if running as root user.
func defaultIsRoot() bool {
	return os.Geteuid() == 0
}

// checkElevation checks whether the process has root rights or sudo command available.
func checkElevation(shell Shell, isRoot bool) error {
	if isRoot {
		return nil
	}
	if _, err := shell.LookPath("sudo"); err != nil {
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

// GoHealer downloads and installs the official Go compiler release into /usr/local/go.
type GoHealer struct {
	Shell   Shell
	Version string
	GOOS    string
	GOARCH  string
	IsRoot  func() bool
}

// NewGoHealer creates a GoHealer with default parameters (Go 1.26.4).
func NewGoHealer(shell Shell) *GoHealer {
	return &GoHealer{Shell: shell, Version: "1.26.4", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *GoHealer) Name() string { return "Go 1.26" }

// CanHeal checks whether the result represents a missing or outdated Go installation.
func (h *GoHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	if name == "go compiler" || name == "go" {
		return true
	}
	if result.Category == "Toolchain" && strings.Contains(name, "go") {
		return true
	}
	return strings.Contains(strings.ToLower(result.Message), "go compiler")
}

// Heal installs Go 1.26 to /usr/local/go and configures environment PATH.
func (h *GoHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	goos := h.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "linux" {
		return fmt.Errorf("go auto-healing is supported on Linux only (detected: %s)", goos)
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
	if err := checkElevation(h.Shell, isRootUser); err != nil {
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
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Go %s: %w\nOutput: %s", version, err, strings.TrimSpace(out))
	}

	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, "/usr/local/go/bin") {
		_ = os.Setenv("PATH", currentPath+":/usr/local/go/bin")
	}
	progress(fmt.Sprintf("Go v%s installed successfully", version))
	return nil
}

// BunHealer executes the official Bun installation script.
type BunHealer struct {
	Shell Shell
}

// NewBunHealer creates a new BunHealer.
func NewBunHealer(shell Shell) *BunHealer { return &BunHealer{Shell: shell} }

// Name returns the identifier of the healer.
func (h *BunHealer) Name() string { return "Bun" }

// CanHeal checks whether the result represents a missing or outdated Bun installation.
func (h *BunHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
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
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Bun: %w\nOutput: %s", err, strings.TrimSpace(out))
	}

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

// NodeHealer installs Node.js 24 LTS via NodeSource package repository.
type NodeHealer struct {
	Shell  Shell
	IsRoot func() bool
}

// NewNodeHealer creates a new NodeHealer.
func NewNodeHealer(shell Shell) *NodeHealer {
	return &NodeHealer{Shell: shell, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *NodeHealer) Name() string { return "Node.js 24 LTS" }

// CanHeal checks whether the result represents a missing or outdated Node.js installation.
func (h *NodeHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
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
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}
	if err := checkElevation(h.Shell, isRootUser); err != nil {
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
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Node.js 24: %w\nOutput: %s", err, strings.TrimSpace(out))
	}
	progress("Node.js 24 LTS installed successfully")
	return nil
}

// GitHealer installs Git and CA certificates via system package manager.
type GitHealer struct {
	Shell  Shell
	IsRoot func() bool
}

// NewGitHealer creates a new GitHealer.
func NewGitHealer(shell Shell) *GitHealer {
	return &GitHealer{Shell: shell, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *GitHealer) Name() string { return "Git" }

// CanHeal checks whether the result represents a missing Git installation.
func (h *GitHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	if strings.Contains(name, "ssh") || strings.Contains(name, "forgejo") || strings.Contains(name, "github") {
		return false
	}
	if name == "git" || strings.Contains(name, "git cli") || strings.Contains(name, "git version") {
		return true
	}
	return strings.Contains(strings.ToLower(result.Message), "git not found") || strings.Contains(strings.ToLower(result.FixSuggestion), "install git")
}

// Heal installs git and ca-certificates using apt-get.
func (h *GitHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}
	if err := checkElevation(h.Shell, isRootUser); err != nil {
		return err
	}
	sp := sudoPrefix(isRootUser)
	progress("Installing git and ca-certificates via apt-get...")
	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
%sapt-get update -y
%sapt-get install -y git ca-certificates
`, sp, sp)
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install git: %w\nOutput: %s", err, strings.TrimSpace(out))
	}
	progress("Git and ca-certificates installed successfully")
	return nil
}

// DockerComposeHealer installs the Docker Compose v2 plugin via apt-get.
type DockerComposeHealer struct {
	Shell  Shell
	IsRoot func() bool
}

// NewDockerComposeHealer creates a new DockerComposeHealer.
func NewDockerComposeHealer(shell Shell) *DockerComposeHealer {
	return &DockerComposeHealer{Shell: shell, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *DockerComposeHealer) Name() string { return "Docker Compose" }

// CanHeal checks whether the result represents a missing Docker Compose plugin.
func (h *DockerComposeHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	if name == "docker compose" || strings.Contains(name, "docker compose") || strings.Contains(name, "docker-compose") {
		return true
	}
	return strings.Contains(strings.ToLower(result.FixSuggestion), "docker-compose-plugin")
}

// Heal installs docker-compose-plugin using apt-get.
func (h *DockerComposeHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}
	if err := checkElevation(h.Shell, isRootUser); err != nil {
		return err
	}
	sp := sudoPrefix(isRootUser)
	progress("Installing docker-compose-plugin via apt-get...")
	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
%sapt-get update -y
%sapt-get install -y docker-compose-plugin 2>/dev/null || %sapt-get install -y docker-compose-v2
`, sp, sp, sp)
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install docker-compose-plugin: %w\nOutput: %s", err, strings.TrimSpace(out))
	}
	progress("Docker Compose plugin installed successfully")
	return nil
}

// DockerDaemonHealer installs Docker Engine and enables the system service.
type DockerDaemonHealer struct {
	Shell  Shell
	IsRoot func() bool
}

// NewDockerDaemonHealer creates a new DockerDaemonHealer.
func NewDockerDaemonHealer(shell Shell) *DockerDaemonHealer {
	return &DockerDaemonHealer{Shell: shell, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *DockerDaemonHealer) Name() string { return "Docker Daemon" }

// CanHeal checks whether the result represents a missing Docker CLI or daemon.
func (h *DockerDaemonHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	if name == "docker daemon" || name == "docker" || strings.Contains(name, "docker daemon") {
		return true
	}
	msg := strings.ToLower(result.Message)
	return strings.Contains(msg, "docker cli not found") || strings.Contains(msg, "docker daemon is not running")
}

// Heal installs Docker Engine, enables the system service, and adds user to docker group.
func (h *DockerDaemonHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}
	if err := checkElevation(h.Shell, isRootUser); err != nil {
		return err
	}
	sp := sudoPrefix(isRootUser)
	progress("Installing Docker Engine and starting docker service...")
	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
%sapt-get update -y
%sapt-get install -y docker.io docker-compose-v2 2>/dev/null || %sapt-get install -y docker-ce docker-compose-plugin 2>/dev/null || true
%ssystemctl enable --now docker 2>/dev/null || true
if [ -n "${USER:-}" ] && [ "$(id -u)" -ne 0 ]; then
	%susermod -aG docker "$USER" 2>/dev/null || true
fi
`, sp, sp, sp, sp, sp)
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Docker Engine: %w\nOutput: %s", err, strings.TrimSpace(out))
	}
	progress("Docker Engine installed and configured successfully")
	return nil
}

// CaddyHealer installs Caddy reverse proxy via official package repository.
type CaddyHealer struct {
	Shell  Shell
	IsRoot func() bool
}

// NewCaddyHealer creates a new CaddyHealer.
func NewCaddyHealer(shell Shell) *CaddyHealer {
	return &CaddyHealer{Shell: shell, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *CaddyHealer) Name() string { return "Caddy Reverse Proxy" }

// CanHeal checks whether the result represents a missing Caddy installation.
func (h *CaddyHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
		return false
	}
	return strings.Contains(strings.ToLower(result.Name), "caddy")
}

// Heal installs Caddy via official apt repository.
func (h *CaddyHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}
	if err := checkElevation(h.Shell, isRootUser); err != nil {
		return err
	}
	sp := sudoPrefix(isRootUser)
	progress("Setting up Caddy official repository and installing caddy...")
	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | %sgpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg 2>/dev/null || true
echo "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" | %stee /etc/apt/sources.list.d/caddy-stable.list >/dev/null 2>&1 || true
%sapt-get update -y
%sapt-get install -y caddy
`, sp, sp, sp, sp)
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Caddy: %w\nOutput: %s", err, strings.TrimSpace(out))
	}
	progress("Caddy reverse proxy installed successfully")
	return nil
}

// TypstHealer downloads and installs the official Typst release binary.
type TypstHealer struct {
	Shell  Shell
	IsRoot func() bool
}

// NewTypstHealer creates a new TypstHealer.
func NewTypstHealer(shell Shell) *TypstHealer {
	return &TypstHealer{Shell: shell, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *TypstHealer) Name() string { return "Typst Compiler" }

// CanHeal checks whether the result represents a missing Typst installation.
func (h *TypstHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
		return false
	}
	return strings.Contains(strings.ToLower(result.Name), "typst")
}

// Heal downloads and installs the official Typst binary release.
func (h *TypstHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}
	if err := checkElevation(h.Shell, isRootUser); err != nil {
		return err
	}
	sp := sudoPrefix(isRootUser)
	progress("Downloading and installing Typst CLI binary release...")
	script := fmt.Sprintf(`set -euo pipefail
TMP_DIR=$(mktemp -d)
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT
curl -fsSL "https://github.com/typst/typst/releases/download/v0.13.0/typst-x86_64-unknown-linux-musl.tar.xz" -o "$TMP_DIR/typst.tar.xz"
tar -xf "$TMP_DIR/typst.tar.xz" -C "$TMP_DIR"
%scp "$TMP_DIR"/typst-*/typst /usr/local/bin/typst 2>/dev/null || cp "$TMP_DIR"/typst-*/typst ~/.local/bin/typst 2>/dev/null
%schmod 755 /usr/local/bin/typst 2>/dev/null || chmod 755 ~/.local/bin/typst 2>/dev/null
`, sp, sp)
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to install Typst: %w\nOutput: %s", err, strings.TrimSpace(out))
	}
	progress("Typst compiler installed successfully")
	return nil
}

// ZshHealer installs zsh and configures user login shell and PATH.
type ZshHealer struct {
	Shell  Shell
	IsRoot func() bool
}

// NewZshHealer creates a new ZshHealer.
func NewZshHealer(shell Shell) *ZshHealer {
	return &ZshHealer{Shell: shell, IsRoot: defaultIsRoot}
}

// Name returns the identifier of the healer.
func (h *ZshHealer) Name() string { return "Zsh & Shell Profile" }

// CanHeal checks whether the result represents a missing zsh, wrong login shell, or PATH issue.
func (h *ZshHealer) CanHeal(result domain.DiagnosticResult) bool {
	if result.Status == domain.StatusOK {
		return false
	}
	name := strings.ToLower(result.Name)
	msg := strings.ToLower(result.Message)
	return strings.Contains(name, "host") || strings.Contains(name, "zsh") || strings.Contains(msg, "zsh") || strings.Contains(msg, "path")
}

// Heal installs zsh, updates login shell, and exports PATH in ~/.zshrc.
func (h *ZshHealer) Heal(ctx context.Context, progress func(step string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	isRootUser := defaultIsRoot()
	if h.IsRoot != nil {
		isRootUser = h.IsRoot()
	}
	if err := checkElevation(h.Shell, isRootUser); err != nil {
		return err
	}
	sp := sudoPrefix(isRootUser)
	progress("Installing zsh and configuring login shell environment...")
	script := fmt.Sprintf(`set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
%sapt-get update -y
%sapt-get install -y zsh
ZSH_PATH=$(command -v zsh 2>/dev/null || echo "/usr/bin/zsh")
if [ -n "${USER:-}" ] && [ "$(id -u)" -ne 0 ]; then
	%schsh -s "$ZSH_PATH" "$USER" 2>/dev/null || chsh -s "$ZSH_PATH" 2>/dev/null || true
fi
mkdir -p "$HOME"
touch "$HOME/.zshrc"
if ! grep -q 'export PATH=.*\.local/bin' "$HOME/.zshrc" 2>/dev/null; then
	echo 'export PATH="$HOME/.local/bin:/usr/local/go/bin:$HOME/.bun/bin:$PATH"' >> "$HOME/.zshrc"
fi
`, sp, sp, sp)
	if out, err := h.Shell.RunShell(ctx, script); err != nil {
		return fmt.Errorf("failed to configure zsh shell: %w\nOutput: %s", err, strings.TrimSpace(out))
	}
	progress("Zsh shell installed and login environment configured successfully")
	return nil
}
