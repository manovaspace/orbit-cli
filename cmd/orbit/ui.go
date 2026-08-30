package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Lipgloss styles for CLI rendering
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("14"))

	boldStyle = lipgloss.NewStyle().
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("10"))

	warningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("11"))

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("9"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))

	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	codeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("13"))

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1).
			MarginTop(1)

	// Unicode status icons
	iconOK    = successStyle.Render("✔")
	iconWarn  = warningStyle.Render("⚠")
	iconError = errorStyle.Render("✖")
	iconInfo  = infoStyle.Render("ℹ")
	iconArrow = subtleStyle.Render("→")
)

// findWorkspaceRoot locates the root of the workspace.
// It checks ORBIT_WORKSPACE, ORBIT_ROOT, or MANOVA_ROOT env vars, walks up parent directories looking for workspace.yaml or .orbit,
// and falls back to the current working directory.
func findWorkspaceRoot(overridePath string) string {
	if overridePath != "" {
		if abs, err := filepath.Abs(overridePath); err == nil {
			if fi, err := os.Stat(abs); err == nil {
				if fi.IsDir() {
					return abs
				}
				return filepath.Dir(abs)
			}
		}
		return overridePath
	}

	for _, envKey := range []string{"ORBIT_WORKSPACE", "ORBIT_ROOT", "MANOVA_ROOT"} {
		if envRoot := os.Getenv(envKey); envRoot != "" {
			if fi, err := os.Stat(envRoot); err == nil && fi.IsDir() {
				return envRoot
			}
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	dir := cwd
	for {
		// Check for workspace.yaml or .orbit
		if _, err := os.Stat(filepath.Join(dir, "workspace.yaml")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".orbit")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return cwd
}

// findManifestPath resolves the path to workspace.yaml based on workspace root and optional flag.
func findManifestPath(workspaceRoot, manifestFlag string) string {
	if manifestFlag != "" {
		if filepath.IsAbs(manifestFlag) {
			return manifestFlag
		}
		// Check if file exists relative to cwd or workspaceRoot
		if _, err := os.Stat(manifestFlag); err == nil {
			if abs, err := filepath.Abs(manifestFlag); err == nil {
				return abs
			}
		}
		return filepath.Join(workspaceRoot, manifestFlag)
	}

	candidates := []string{
		filepath.Join(workspaceRoot, "workspace.yaml"),
		filepath.Join(workspaceRoot, "orbit", "orbit-cli", "workspace.yaml"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return filepath.Join(workspaceRoot, "workspace.yaml")
}

// renderCard wraps text inside a clean border box.
func renderCard(title, content string) string {
	var sb strings.Builder
	if title != "" {
		sb.WriteString(headerStyle.Render(title) + "\n\n")
	}
	sb.WriteString(content)
	return cardStyle.Render(sb.String())
}

// padRight pads a string with spaces up to the specified width.
func padRight(s string, width int) string {
	length := lipgloss.Width(s)
	if length >= width {
		return s
	}
	return s + strings.Repeat(" ", width-length)
}

// promptConfirm displays an interactive confirmation prompt defaulting to true (Y/n) or false (y/N).
func promptConfirm(in io.Reader, out io.Writer, prompt string, defaultVal bool) bool {
	suffix := " (Y/n) [Y]: "
	if !defaultVal {
		suffix = " (y/N) [N]: "
	}
	fmt.Fprint(out, boldStyle.Render(prompt)+subtleStyle.Render(suffix))

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil {
		return defaultVal
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultVal
	}
	return line == "y" || line == "yes"
}
