package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/updater"
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

// findWorkspaceRoot locates the root of the Manova workspace.
// It checks MANOVA_ROOT env var, walks up parent directories looking for workspace.yaml or .manova or .git,
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

	if envRoot := os.Getenv("MANOVA_ROOT"); envRoot != "" {
		if fi, err := os.Stat(envRoot); err == nil && fi.IsDir() {
			return envRoot
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	dir := cwd
	for {
		// Check for workspace.yaml or .manova or handbook directory
		if _, err := os.Stat(filepath.Join(dir, "workspace.yaml")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".manova")); err == nil {
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

// renderUpdateBanner formats the new version notification card with Top 5 release highlights.
func renderUpdateBanner(currentVersion, latestVersion string, highlights []string) string {
	var sb strings.Builder

	cur := updater.FormatVersion(currentVersion)
	latest := updater.FormatVersion(latestVersion)

	header := fmt.Sprintf("%s %s %s %s %s",
		iconInfo,
		boldStyle.Render("New release available:"),
		codeStyle.Render(cur),
		iconArrow,
		successStyle.Render(latest),
	)
	sb.WriteString(header)

	// Truncate highlights to Top 5
	hl := highlights
	if len(hl) > 5 {
		hl = hl[:5]
	}

	if len(hl) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(headerStyle.Render("Release Highlights:"))
		for _, item := range hl {
			sb.WriteString("\n  • " + item)
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Run '%s' to update.", boldStyle.Render("manova self-update")))

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

