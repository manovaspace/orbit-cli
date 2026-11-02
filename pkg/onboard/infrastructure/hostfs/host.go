// Package hostfs is the local machine and filesystem adapter of the onboarding
// flow. It writes the MCP env file, shell profiles and VPN profile and probes
// the host (home, hostname, DNS, local Caddy CA).
package hostfs

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	migrateworkspace "github.com/manovaspace/orbit-cli/pkg/migrate/infrastructure/workspace"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Host is the local machine and filesystem.
type Host struct {
	Runner runner.Runner
	// LookPath defaults to exec.LookPath; tests replace it to simulate tools.
	LookPath func(string) (string, error)
}

// New builds a Host over r.
func New(r runner.Runner) *Host {
	return &Host{Runner: r}
}

// HomeDir returns the user's home directory.
func (h *Host) HomeDir() (string, error) {
	return os.UserHomeDir()
}

// Hostname returns the machine hostname, or "unknown-host" when it cannot be
// read.
func (h *Host) Hostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return hostname
}

// Platform returns the running OS and architecture.
func (h *Host) Platform() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}

// Getenv reads one environment variable.
func (h *Host) Getenv(key string) string {
	return os.Getenv(key)
}

// LookupHost resolves a DNS name.
func (h *Host) LookupHost(name string) ([]string, error) {
	return net.LookupHost(name)
}

// TrustDevCA trusts the local Caddy root CA. present is false when caddy is not
// installed; trusted is false when trust failed, typically for lack of
// privileges.
func (h *Host) TrustDevCA(ctx context.Context) (present, trusted bool) {
	lookPath := h.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath("caddy"); err != nil {
		return false, false
	}
	if h.Runner == nil {
		return true, false
	}
	_, err := h.Runner.Run(ctx, "", "caddy", "trust")
	return true, err == nil
}

// EnsureWorkspaceDirs creates the standard workspace directory tree.
func (h *Host) EnsureWorkspaceDirs(root string) error {
	return migrateworkspace.New(h.Runner).EnsureWorkspaceDirs(root)
}

// WriteWireGuard saves the VPN profile under ~/.config/orbit/wg0.conf.
func (h *Host) WriteWireGuard(configData string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	wgDir := filepath.Join(home, ".config", "orbit")
	if err := os.MkdirAll(wgDir, 0700); err != nil {
		return "", err
	}
	wgPath := filepath.Join(wgDir, "wg0.conf")
	if err := os.WriteFile(wgPath, []byte(configData), 0600); err != nil {
		return "", err
	}
	return wgPath, nil
}

// ConfigureMCPEnv writes the git token and UID into .cursor/mcp.env and links
// the Cursor rules. The token is never printed.
func (h *Host) ConfigureMCPEnv(workspaceRoot, token, uid string) error {
	cursorDir := filepath.Join(workspaceRoot, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return fmt.Errorf("failed to create .cursor directory: %w", err)
	}

	bootstrap := migrateworkspace.New(h.Runner)
	mcpEnvPath := filepath.Join(cursorDir, "mcp.env")
	existingContent := ""
	if data, err := os.ReadFile(mcpEnvPath); err == nil {
		existingContent = string(data)
	} else {
		_ = bootstrap.SetupMCPEnvironment(workspaceRoot)
		if data, err := os.ReadFile(mcpEnvPath); err == nil {
			existingContent = string(data)
		}
	}

	lines := strings.Split(existingContent, "\n")
	foundToken := false
	foundMCPToken := false
	foundUID := false
	var newLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "FORGEJO_TOKEN="):
			newLines = append(newLines, fmt.Sprintf("FORGEJO_TOKEN=%s", token))
			foundToken = true
		case strings.HasPrefix(trimmed, "FORGEJO_MCP_TOKEN="):
			newLines = append(newLines, fmt.Sprintf("FORGEJO_MCP_TOKEN=%s", token))
			foundMCPToken = true
		case strings.HasPrefix(trimmed, "MANOVA_USER_UID="):
			newLines = append(newLines, fmt.Sprintf("MANOVA_USER_UID=%s", uid))
			foundUID = true
		default:
			newLines = append(newLines, line)
		}
	}

	if !foundToken && token != "" {
		newLines = append(newLines, fmt.Sprintf("FORGEJO_TOKEN=%s", token))
	}
	if !foundMCPToken && token != "" {
		newLines = append(newLines, fmt.Sprintf("FORGEJO_MCP_TOKEN=%s", token))
	}
	if !foundUID && uid != "" {
		newLines = append(newLines, fmt.Sprintf("MANOVA_USER_UID=%s", uid))
	}

	finalData := strings.Join(newLines, "\n")
	if !strings.HasSuffix(finalData, "\n") {
		finalData += "\n"
	}

	if err := os.WriteFile(mcpEnvPath, []byte(finalData), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", mcpEnvPath, err)
	}

	_ = bootstrap.SymlinkCursorRules(workspaceRoot)
	return nil
}

// ConfigureShell appends the Go proxy and private-module settings to the shell
// profiles when they are missing.
func (h *Host) ConfigureShell(homeDir string) error {
	if homeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home dir: %w", err)
		}
		homeDir = home
	}

	rcFiles := []string{".zshrc", ".bashrc"}
	goprivateLine := `export GOPRIVATE="git.internal.manova.space/*"`
	goproxyLine := `export GOPROXY="https://proxy.golang.org,direct"`

	for _, rc := range rcFiles {
		rcPath := filepath.Join(homeDir, rc)
		var existingContent string
		if data, err := os.ReadFile(rcPath); err == nil {
			existingContent = string(data)
		}

		var additions []string
		if !strings.Contains(existingContent, "GOPRIVATE=") && !strings.Contains(existingContent, "git.internal.manova.space") {
			additions = append(additions, goprivateLine)
		}
		if !strings.Contains(existingContent, "GOPROXY=") {
			additions = append(additions, goproxyLine)
		}

		if len(additions) > 0 {
			newContent := existingContent
			if newContent != "" && !strings.HasSuffix(newContent, "\n") {
				newContent += "\n"
			}
			newContent += strings.Join(additions, "\n") + "\n"
			if err := os.WriteFile(rcPath, []byte(newContent), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", rcPath, err)
			}
		}
	}
	return nil
}

// Exists reports whether path exists.
func (h *Host) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
func (h *Host) IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// Remove deletes path and everything below it.
func (h *Host) Remove(path string) error {
	return os.RemoveAll(path)
}
