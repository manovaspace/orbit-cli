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
		case "Zsh Shell":
			if err := InstallZsh(ctx, pm, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to auto-install Zsh: %v\n", err)
			}
		case "Oh My Zsh":
			if err := InstallOhMyZsh(ctx, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to auto-install Oh My Zsh: %v\n", err)
			}
		case "Default Login Shell":
			if err := SetDefaultShellZsh(ctx, out); err != nil {
				fmt.Fprintf(out, "  ✖ Failed to set default shell to Zsh: %v\n", err)
			}
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

// InstallZsh installs Zsh using the detected package manager.
func InstallZsh(ctx context.Context, pm PackageManager, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Installing Zsh shell...")

	var cmd *exec.Cmd
	switch pm {
	case PkgApt:
		if os.Geteuid() == 0 {
			_ = exec.CommandContext(ctx, "apt-get", "update", "-y").Run()
			cmd = exec.CommandContext(ctx, "apt-get", "install", "-y", "zsh", "curl", "git")
		} else {
			_ = exec.CommandContext(ctx, "sudo", "apt-get", "update", "-y").Run()
			cmd = exec.CommandContext(ctx, "sudo", "apt-get", "install", "-y", "zsh", "curl", "git")
		}
	case PkgBrew:
		cmd = exec.CommandContext(ctx, "brew", "install", "zsh")
	case PkgDnf:
		if os.Geteuid() == 0 {
			cmd = exec.CommandContext(ctx, "dnf", "install", "-y", "zsh")
		} else {
			cmd = exec.CommandContext(ctx, "sudo", "dnf", "install", "-y", "zsh")
		}
	case PkgPacman:
		if os.Geteuid() == 0 {
			cmd = exec.CommandContext(ctx, "pacman", "-S", "--noconfirm", "zsh")
		} else {
			cmd = exec.CommandContext(ctx, "sudo", "pacman", "-S", "--noconfirm", "zsh")
		}
	default:
		return fmt.Errorf("unsupported package manager (%s) for automatic Zsh install", pm)
	}

	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install Zsh: %w", err)
	}

	fmt.Fprintln(out, "  ✔ Zsh installed successfully.")
	return nil
}

// InstallOhMyZsh installs Oh My Zsh framework in unattended mode.
func InstallOhMyZsh(ctx context.Context, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Installing Oh My Zsh framework...")

	cmd := exec.CommandContext(ctx, "sh", "-c", "RUNZSH=no CHSH=no sh -c \"$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)\" \"\" --unattended")
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install Oh My Zsh: %w", err)
	}

	fmt.Fprintln(out, "  ✔ Oh My Zsh framework installed successfully.")
	return nil
}

// SetDefaultShellZsh sets Zsh as the user's default login shell.
func SetDefaultShellZsh(ctx context.Context, out io.Writer) error {
	fmt.Fprintln(out, "  ⠋ Setting default login shell to Zsh...")

	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		zshPath = "/usr/bin/zsh"
	}

	user := os.Getenv("USER")
	if user == "" {
		userCmd := exec.CommandContext(ctx, "whoami")
		if uOut, err := userCmd.Output(); err == nil {
			user = strings.TrimSpace(string(uOut))
		}
	}

	cmd := exec.CommandContext(ctx, "chsh", "-s", zshPath)
	if err := cmd.Run(); err != nil {
		if user != "" {
			sudoCmd := exec.CommandContext(ctx, "sudo", "chsh", "-s", zshPath, user)
			sudoCmd.Stdout = out
			sudoCmd.Stderr = out
			if err := sudoCmd.Run(); err != nil {
				return fmt.Errorf("failed to change default shell: %w", err)
			}
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		_ = EnsureZshConfigured(home)
	}

	fmt.Fprintln(out, "  ✔ Default login shell set to Zsh.")
	return nil
}

// EnsureZshConfigured configures ~/.zshrc with Oh My Zsh defaults and Manova alias/completion.
func EnsureZshConfigured(home string) error {
	zshrcPath := filepath.Join(home, ".zshrc")
	omzTemplate := filepath.Join(home, ".oh-my-zsh", "templates", "zshrc.zsh-template")

	if _, err := os.Stat(zshrcPath); os.IsNotExist(err) {
		if data, err := os.ReadFile(omzTemplate); err == nil {
			_ = os.WriteFile(zshrcPath, data, 0644)
		}
	}

	data, err := os.ReadFile(zshrcPath)
	content := string(data)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var additions []string
	if !strings.Contains(content, "alias m=") {
		additions = append(additions, "\n# Manova CLI shortcut\nalias m=\"manova\"")
	}
	if !strings.Contains(content, "# Manova CLI Autocompletion") {
		additions = append(additions, "\n# Manova CLI Autocompletion\nif command -v manova >/dev/null 2>&1; then\n  source <(manova completion zsh)\n  compdef m=manova 2>/dev/null || true\nfi")
	}

	if len(additions) > 0 {
		f, err := os.OpenFile(zshrcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		for _, a := range additions {
			if _, err := f.WriteString(a + "\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

// WarmUpSudo runs 'sudo -v' for non-root users to authenticate once and cache credential timestamp.
func WarmUpSudo(ctx context.Context, in io.Reader, out io.Writer) error {
	if os.Geteuid() == 0 {
		return nil
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "sudo", "-v")
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// CreateDevUser creates a dedicated development user with sudo and docker group membership,
// sets default shell to Zsh, copies root SSH keys, and configures .zshrc.
// If the user already exists, it ensures sudo privileges, SSH keys, and Zsh configuration are active.
func CreateDevUser(ctx context.Context, username string, out io.Writer) error {
	if username == "" {
		username = "dev"
	}

	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		zshPath = "/usr/bin/zsh"
	}

	// 1. Check if user already exists
	userExists := false
	if _, err := exec.CommandContext(ctx, "id", "-u", username).Output(); err == nil {
		userExists = true
	}

	if userExists {
		fmt.Fprintf(out, "  ℹ Developer user '%s' already exists. Configuring sudo, SSH keys, and Zsh shell...\n", username)
		// Ensure default login shell is Zsh
		_ = exec.CommandContext(ctx, "usermod", "-s", zshPath, username).Run()
	} else {
		fmt.Fprintf(out, "  ⠋ Creating dedicated developer user '%s'...\n", username)
		cmdAdd := exec.CommandContext(ctx, "useradd", "-m", "-s", zshPath, username)
		if outBytes, err := cmdAdd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create user %s: %w (%s)", username, err, strings.TrimSpace(string(outBytes)))
		}
	}

	// 2. Add user to sudo, docker, and wheel groups
	for _, grp := range []string{"sudo", "docker", "wheel"} {
		_ = exec.CommandContext(ctx, "usermod", "-aG", grp, username).Run()
	}

	// 3. Configure sudoers rule (/etc/sudoers.d/90-manova-dev)
	sudoersContent := fmt.Sprintf("%s ALL=(ALL) NOPASSWD:ALL\n", username)
	sudoersFile := filepath.Join("/etc/sudoers.d", fmt.Sprintf("90-manova-%s", username))
	_ = os.WriteFile(sudoersFile, []byte(sudoersContent), 0440)

	// 4. Resolve user home directory
	devHome := filepath.Join("/home", username)
	if outBytes, err := exec.CommandContext(ctx, "getent", "passwd", username).Output(); err == nil {
		parts := strings.Split(strings.TrimSpace(string(outBytes)), ":")
		if len(parts) >= 6 && parts[5] != "" {
			devHome = parts[5]
		}
	}

	// Copy root SSH authorized_keys if present and user doesn't already have them
	devSSHDir := filepath.Join(devHome, ".ssh")
	devAuthKeys := filepath.Join(devSSHDir, "authorized_keys")
	rootAuthKeys := "/root/.ssh/authorized_keys"

	if _, err := os.Stat(devAuthKeys); os.IsNotExist(err) {
		if rootKeys, err := os.ReadFile(rootAuthKeys); err == nil && len(rootKeys) > 0 {
			_ = os.MkdirAll(devSSHDir, 0700)
			_ = os.WriteFile(devAuthKeys, rootKeys, 0600)
		}
	}

	// 5. Ensure .zshrc in dev home
	_ = EnsureZshConfigured(devHome)

	// 6. Fix ownership of dev home directory
	_ = exec.CommandContext(ctx, "chown", "-R", fmt.Sprintf("%s:%s", username, username), devHome).Run()

	if userExists {
		fmt.Fprintf(out, "  ✔ Dedicated developer user '%s' configured with sudo privileges and Zsh shell.\n", username)
	} else {
		fmt.Fprintf(out, "  ✔ Dedicated developer user '%s' created and configured.\n", username)
	}
	return nil
}
