package doctor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// PackageManager represents the detected system package manager.
type PackageManager string

const (
	PkgApt     PackageManager = "apt"
	PkgBrew    PackageManager = "brew"
	PkgDnf     PackageManager = "dnf"
	PkgPacman  PackageManager = "pacman"
	PkgUnknown PackageManager = "unknown"
)

// DetectPackageManager determines the system package manager available in PATH.
func DetectPackageManager() PackageManager {
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("brew"); err == nil {
			return PkgBrew
		}
	}
	if _, err := exec.LookPath("apt-get"); err == nil {
		return PkgApt
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return PkgDnf
	}
	if _, err := exec.LookPath("pacman"); err == nil {
		return PkgPacman
	}
	if _, err := exec.LookPath("brew"); err == nil {
		return PkgBrew
	}
	return PkgUnknown
}

// AutoInstallDependencies inspects the DoctorReport for missing tools and installs them.
func AutoInstallDependencies(ctx context.Context, report *DoctorReport, out io.Writer) error {
	if report == nil {
		return nil
	}

	pm := DetectPackageManager()

	for _, res := range report.Results {
		if res.Status == StatusOK {
			continue
		}

		switch res.Name {
		case "Go Compiler":
			if err := InstallGo(ctx, pm, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to auto-install Go: %v\n", err)
			}
		case "Bun":
			if err := InstallBun(ctx, pm, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to auto-install Bun: %v\n", err)
			}
		case "Node.js":
			if err := InstallNode(ctx, pm, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to auto-install Node.js: %v\n", err)
			}
		case "Docker Daemon", "Docker Compose":
			if err := InstallDocker(ctx, pm, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to auto-install Docker: %v\n", err)
			}
		case "SSH Agent":
			if err := StartSSHAgent(ctx, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to start SSH agent: %v\n", err)
			}
		}
	}

	return nil
}

// InstallBun installs Bun using the official installation script and updates the process PATH.
func InstallBun(ctx context.Context, pm PackageManager, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Installing Bun JavaScript runtime...")

	// 1. Run official installer
	cmd := exec.CommandContext(ctx, "bash", "-c", "curl -fsSL https://bun.sh/install | bash")
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bun installer failed: %w", err)
	}

	// 2. Add to active process PATH
	home, _ := os.UserHomeDir()
	bunBin := filepath.Join(home, ".bun", "bin")
	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, bunBin) {
		_ = os.Setenv("PATH", bunBin+":"+currentPath)
		_ = os.Setenv("BUN_INSTALL", filepath.Join(home, ".bun"))
	}

	fmt.Fprintln(out, "  ✔ Bun installed successfully.")
	return nil
}

// InstallNode installs Node.js 22 LTS via package manager.
func InstallNode(ctx context.Context, pm PackageManager, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Installing Node.js LTS...")

	switch pm {
	case PkgApt:
		script := "curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash - && sudo apt-get install -y nodejs"
		cmd := exec.CommandContext(ctx, "bash", "-c", script)
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("apt nodejs install failed: %w", err)
		}
	case PkgBrew:
		cmd := exec.CommandContext(ctx, "brew", "install", "node@22")
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("brew node install failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported package manager for automatic Node.js install (%s)", pm)
	}

	fmt.Fprintln(out, "  ✔ Node.js installed successfully.")
	return nil
}

// InstallGo installs the Go compiler.
func InstallGo(ctx context.Context, pm PackageManager, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Installing Go compiler...")

	switch pm {
	case PkgApt:
		cmd := exec.CommandContext(ctx, "sudo", "apt-get", "install", "-y", "golang-go")
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("apt golang install failed: %w", err)
		}
	case PkgBrew:
		cmd := exec.CommandContext(ctx, "brew", "install", "go")
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("brew go install failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported package manager for automatic Go install (%s)", pm)
	}

	home, _ := os.UserHomeDir()
	goBin := filepath.Join(home, "go", "bin")
	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, goBin) {
		_ = os.Setenv("PATH", currentPath+":"+goBin)
	}

	fmt.Fprintln(out, "  ✔ Go compiler installed successfully.")
	return nil
}

// InstallDocker installs Docker Engine and Docker Compose v2.
func InstallDocker(ctx context.Context, pm PackageManager, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Installing Docker and Docker Compose...")

	switch pm {
	case PkgApt:
		cmd := exec.CommandContext(ctx, "sudo", "apt-get", "install", "-y", "docker.io", "docker-compose-v2")
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("apt docker install failed: %w", err)
		}

		// Add current user to docker group if possible
		user := os.Getenv("USER")
		if user == "" {
			user = os.Getenv("LOGNAME")
		}
		if user != "" {
			_ = exec.CommandContext(ctx, "sudo", "usermod", "-aG", "docker", user).Run()
		}
	case PkgBrew:
		fmt.Fprintln(out, "  ℹ On macOS, please install Docker Desktop from https://www.docker.com/products/docker-desktop/")
		return nil
	default:
		return fmt.Errorf("unsupported package manager for automatic Docker install (%s)", pm)
	}

	fmt.Fprintln(out, "  ✔ Docker installed successfully.")
	return nil
}

// StartSSHAgent starts a new SSH agent process and sets SSH_AUTH_SOCK in the active environment.
func StartSSHAgent(ctx context.Context, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Starting SSH agent...")

	cmd := exec.CommandContext(ctx, "ssh-agent", "-s")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run ssh-agent: %w", err)
	}

	// Parse output: SSH_AUTH_SOCK=/tmp/ssh-xxx/agent.123; export SSH_AUTH_SOCK; ...
	output := buf.String()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSH_AUTH_SOCK=") {
			val := strings.TrimPrefix(line, "SSH_AUTH_SOCK=")
			val = strings.Split(val, ";")[0]
			_ = os.Setenv("SSH_AUTH_SOCK", val)
		}
		if strings.HasPrefix(line, "SSH_AGENT_PID=") {
			val := strings.TrimPrefix(line, "SSH_AGENT_PID=")
			val = strings.Split(val, ";")[0]
			_ = os.Setenv("SSH_AGENT_PID", val)
		}
	}

	if os.Getenv("SSH_AUTH_SOCK") != "" {
		fmt.Fprintln(out, "  ✔ SSH agent started.")
		return nil
	}

	return fmt.Errorf("unable to determine SSH_AUTH_SOCK from agent output: %s", output)
}
