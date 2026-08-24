package alias

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsCommandTaken checks if a command name is already in PATH or defined as an alias in shell RC files.
func IsCommandTaken(name string) (bool, string) {
	// 1. Check if executable exists in PATH
	if path, err := exec.LookPath(name); err == nil {
		return true, fmt.Sprintf("binary in PATH (%s)", path)
	}

	// 2. Check common shell config files for existing alias or function definitions
	home, err := os.UserHomeDir()
	if err != nil {
		return false, ""
	}

	rcFiles := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".profile"),
		filepath.Join(home, ".bash_profile"),
	}

	aliasPrefix1 := fmt.Sprintf("alias %s=", name)
	aliasPrefix2 := fmt.Sprintf("alias %s =", name)
	funcPrefix := fmt.Sprintf("%s()", name)

	for _, rc := range rcFiles {
		data, err := os.ReadFile(rc)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, aliasPrefix1) || strings.HasPrefix(trimmed, aliasPrefix2) || strings.HasPrefix(trimmed, funcPrefix) {
				return true, fmt.Sprintf("defined in %s (%s)", filepath.Base(rc), trimmed)
			}
		}
	}

	return false, ""
}

// DetectTargetRCFile returns the most appropriate shell RC file based on $SHELL and existing files.
func DetectTargetRCFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "zsh") {
		zshrc := filepath.Join(home, ".zshrc")
		return zshrc
	}
	if strings.Contains(shell, "bash") {
		bashrc := filepath.Join(home, ".bashrc")
		return bashrc
	}

	// Fallbacks
	zshrc := filepath.Join(home, ".zshrc")
	if _, err := os.Stat(zshrc); err == nil {
		return zshrc
	}
	bashrc := filepath.Join(home, ".bashrc")
	if _, err := os.Stat(bashrc); err == nil {
		return bashrc
	}

	return filepath.Join(home, ".profile")
}

// AddShellAlias safely appends an alias definition to the user's active shell RC file.
func AddShellAlias(aliasName, targetCmd string) (string, error) {
	rcPath := DetectTargetRCFile()
	if rcPath == "" {
		return "", fmt.Errorf("unable to determine user shell profile file")
	}

	aliasLine := fmt.Sprintf("alias %s=%q", aliasName, targetCmd)

	// Check if already in target file
	if data, err := os.ReadFile(rcPath); err == nil {
		content := string(data)
		if strings.Contains(content, aliasLine) || strings.Contains(content, fmt.Sprintf("alias %s='%s'", aliasName, targetCmd)) {
			return rcPath, nil // Already present
		}
	}

	// Append alias to RC file
	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %w", rcPath, err)
	}
	defer f.Close()

	entry := fmt.Sprintf("\n# Manova CLI shortcut\n%s\n", aliasLine)
	if _, err := f.WriteString(entry); err != nil {
		return "", fmt.Errorf("failed to write alias to %s: %w", rcPath, err)
	}

	return rcPath, nil
}
