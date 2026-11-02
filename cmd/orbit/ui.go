package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/manovaspace/orbit-cli/pkg/render"
)

// Aliases for the shared render styles. They keep the historical unexported
// names so existing call sites stay unchanged until ADR-047 phase 3.
var (
	titleStyle   = render.TitleLegacy
	headerStyle  = render.Header
	boldStyle    = render.Bold
	successStyle = render.Success
	warningStyle = render.Warning
	errorStyle   = render.Error
	infoStyle    = render.Info
	subtleStyle  = render.Subtle
	dimStyle     = render.Subtle
	codeStyle    = render.Code
	cardStyle    = render.Card

	// Unicode status icons
	iconOK    = render.IconOK
	iconWarn  = render.IconWarn
	iconError = render.IconError
	iconInfo  = render.IconInfo
	iconArrow = render.IconArrow
)

// findWorkspaceRoot locates the root of the workspace.
// It checks ORBIT_WORKSPACE, ORBIT_ROOT, or MANOVA_ROOT env vars, walks up parent directories looking for workspace.yaml or .orbit,
// and falls back to the current working directory.
func findWorkspaceRoot(overridePath string) string {
	if overridePath != "" {
		if abs, err := filepath.Abs(overridePath); err == nil {
			dir := abs
			if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
				dir = filepath.Dir(dir)
			}
			for {
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
			return abs
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

// promptYesNo asks a yes/no question and returns defaultYes on an empty answer
// or end of input.
func promptYesNo(r io.Reader, w io.Writer, question string, defaultYes bool) bool {
	var defStr string
	if defaultYes {
		defStr = "[Y/n]"
	} else {
		defStr = "[y/N]"
	}
	fmt.Fprintf(w, "%s %s: ", question, subtleStyle.Render(defStr))
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		text := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if text == "" {
			return defaultYes
		}
		return text == "y" || text == "yes"
	}
	return defaultYes
}

// promptString asks for a line of input, returning defaultValue when the answer
// is empty or input ends.
func promptString(r io.Reader, w io.Writer, question, defaultValue string) string {
	if defaultValue != "" {
		fmt.Fprintf(w, "%s %s: ", question, subtleStyle.Render("("+defaultValue+")"))
	} else {
		fmt.Fprintf(w, "%s: ", question)
	}
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return defaultValue
		}
		return text
	}
	return defaultValue
}
