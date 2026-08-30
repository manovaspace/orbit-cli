package onboard

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

// EnvStepItem represents a single automation step in the environment setup stage.
type EnvStepItem struct {
	Name        string
	Description string
	Status      string  // "pending", "running", "done", "failed", "skipped"
	Error       string
	Progress    float64
	Required    bool
}

// EnvStepResult is returned by the automation runner for each step.
type EnvStepResult struct {
	Name    string
	Success bool
	Error   string
}

// EnvAutomationFinishedMsg is dispatched when all automation steps complete.
type EnvAutomationFinishedMsg struct {
	Results []EnvStepResult
	Err     error
}

// EnvAutomationRunnerFunc is the injectable runner for testability.
type EnvAutomationRunnerFunc func(workspaceRoot string, steps []EnvStepItem, parent *WizardModel) []EnvStepResult

// SetupWorkspaceEnvironment symlinks Cursor rules/skills from handbook into .cursor/.
func SetupWorkspaceEnvironment(workspaceRoot string) error {
	return migrate.SymlinkCursorRules(workspaceRoot)
}

// ConfigureMCPEnvironment writes/updates .cursor/mcp.env with Forgejo credentials.
func ConfigureMCPEnvironment(workspaceRoot string, token string, uid string) error {
	cursorDir := filepath.Join(workspaceRoot, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return fmt.Errorf("failed to create .cursor directory: %w", err)
	}

	mcpEnvPath := filepath.Join(cursorDir, "mcp.env")

	existingContent := ""
	if _, err := os.Stat(mcpEnvPath); err == nil {
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
		if strings.HasPrefix(trimmed, "FORGEJO_TOKEN=") {
			newLines = append(newLines, fmt.Sprintf("FORGEJO_TOKEN=%s", token))
			foundToken = true
		} else if strings.HasPrefix(trimmed, "FORGEJO_MCP_TOKEN=") {
			newLines = append(newLines, fmt.Sprintf("FORGEJO_MCP_TOKEN=%s", token))
			foundMCPToken = true
		} else if strings.HasPrefix(trimmed, "MANOVA_USER_UID=") {
			newLines = append(newLines, fmt.Sprintf("MANOVA_USER_UID=%s", uid))
			foundUID = true
		} else {
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
	return nil
}

// EnsureWireGuardConfig writes WireGuard config to ~/.config/orbit/wg0.conf.
// Returns empty string if configData is empty (no-op).
func EnsureWireGuardConfig(configData string) (string, error) {
	if strings.TrimSpace(configData) == "" {
		return "", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}
	wgDir := filepath.Join(home, ".config", "orbit")
	if err := os.MkdirAll(wgDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create orbit config dir: %w", err)
	}
	wgPath := filepath.Join(wgDir, "wg0.conf")
	if err := os.WriteFile(wgPath, []byte(configData), 0600); err != nil {
		return "", fmt.Errorf("failed to write wg0.conf: %w", err)
	}
	return wgPath, nil
}

// defaultEnvSteps returns the standard automation checklist.
func defaultEnvSteps() []EnvStepItem {
	return []EnvStepItem{
		{
			Name:        "Cursor MCP & Rules",
			Description: "Symlink handbook Cursor rules/skills and configure .cursor/mcp.env",
			Status:      "pending",
			Required:    true,
		},
		{
			Name:        "WireGuard VPN Profile",
			Description: "Write VPN credentials to ~/.config/orbit/wg0.conf",
			Status:      "pending",
			Required:    false,
		},
		{
			Name:        "Local Dev DNS",
			Description: "Verify /etc/hosts entries for *.dev.manova.space",
			Status:      "pending",
			Required:    false,
		},
		{
			Name:        "Go Proxy & GOPRIVATE",
			Description: "Verify GOPRIVATE includes git.dev.manova.space",
			Status:      "pending",
			Required:    false,
		},
	}
}

// defaultEnvRunner is the production automation runner that executes each step.
func defaultEnvRunner(workspaceRoot string, steps []EnvStepItem, parent *WizardModel) []EnvStepResult {
	results := make([]EnvStepResult, len(steps))

	token := ""
	uid := ""
	wgConfig := ""
	if parent != nil && parent.Session != nil {
		token = parent.Session.ForgejoToken
		uid = parent.Session.UID
		wgConfig = parent.Session.WireGuardConfig
	}

	for i, step := range steps {
		switch step.Name {
		case "Cursor MCP & Rules":
			var errs []string
			if err := SetupWorkspaceEnvironment(workspaceRoot); err != nil {
				errs = append(errs, fmt.Sprintf("symlink: %v", err))
			}
			if err := ConfigureMCPEnvironment(workspaceRoot, token, uid); err != nil {
				errs = append(errs, fmt.Sprintf("mcp.env: %v", err))
			}
			if len(errs) > 0 {
				results[i] = EnvStepResult{Name: step.Name, Success: false, Error: strings.Join(errs, "; ")}
			} else {
				results[i] = EnvStepResult{Name: step.Name, Success: true}
			}

		case "WireGuard VPN Profile":
			if _, err := EnsureWireGuardConfig(wgConfig); err != nil {
				results[i] = EnvStepResult{Name: step.Name, Success: false, Error: err.Error()}
			} else {
				results[i] = EnvStepResult{Name: step.Name, Success: true}
			}

		case "Local Dev DNS":
			addrs, err := net.LookupHost("dev.manova.space")
			if err != nil || len(addrs) == 0 {
				results[i] = EnvStepResult{Name: step.Name, Success: false, Error: "dev.manova.space not resolvable (add to /etc/hosts or configure DNS)"}
			} else {
				results[i] = EnvStepResult{Name: step.Name, Success: true}
			}

		case "Go Proxy & GOPRIVATE":
			goprivate := os.Getenv("GOPRIVATE")
			if strings.Contains(goprivate, "git.dev.manova.space") {
				results[i] = EnvStepResult{Name: step.Name, Success: true}
			} else {
				// Non-fatal: just note it
				results[i] = EnvStepResult{Name: step.Name, Success: true, Error: "GOPRIVATE not set (add GOPRIVATE=git.dev.manova.space/* to shell profile)"}
			}

		default:
			results[i] = EnvStepResult{Name: step.Name, Success: true}
		}
	}
	return results
}

// EnvModel manages the Stage 5 Environment automation view.
type EnvModel struct {
	parent    *WizardModel
	steps     []EnvStepItem
	running   bool
	hasError  bool
	isSkipped bool
	results   []EnvStepResult
	spinner   spinner.Model
	width     int
	runner    EnvAutomationRunnerFunc
}

// NewEnvModel creates a new EnvModel for the Environment stage.
func NewEnvModel(parent *WizardModel) *EnvModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	w := 80
	if parent != nil && parent.Width > 0 {
		w = parent.Width
	}

	return &EnvModel{
		parent:  parent,
		steps:   defaultEnvSteps(),
		running: false,
		spinner: sp,
		width:   w,
		runner:  defaultEnvRunner,
	}
}

// Steps returns the current automation step items.
func (m *EnvModel) Steps() []EnvStepItem {
	return m.steps
}

// SetAutomationRunner injects a custom runner (for testing).
func (m *EnvModel) SetAutomationRunner(fn EnvAutomationRunnerFunc) {
	m.runner = fn
}

// SetHasError sets the error state directly (for testing).
func (m *EnvModel) SetHasError(v bool) {
	m.hasError = v
}

// Init returns a spinner tick command.
func (m *EnvModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// RunAutomation dispatches the async automation runner as a tea.Cmd.
func (m *EnvModel) RunAutomation() tea.Cmd {
	m.running = true
	m.hasError = false

	// Reset step statuses
	for i := range m.steps {
		m.steps[i].Status = "pending"
		m.steps[i].Error = ""
	}

	workspaceRoot := ""
	if m.parent != nil && m.parent.Session != nil && m.parent.Session.WorkspacePath != "" {
		workspaceRoot = m.parent.Session.WorkspacePath
	}
	if workspaceRoot == "" {
		if env := os.Getenv("ORBIT_WORKSPACE"); env != "" {
			workspaceRoot = env
		} else {
			workspaceRoot, _ = os.Getwd()
		}
	}

	steps := m.steps
	parent := m.parent
	runner := m.runner

	return func() tea.Msg {
		results := runner(workspaceRoot, steps, parent)
		return EnvAutomationFinishedMsg{Results: results}
	}
}

// Update handles Bubble Tea messages for the EnvModel.
func (m *EnvModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case EnvAutomationFinishedMsg:
		m.running = false
		m.results = msg.Results

		// Update step statuses from results
		resultMap := make(map[string]EnvStepResult, len(msg.Results))
		for _, r := range msg.Results {
			resultMap[r.Name] = r
		}
		hasFailure := false
		for i, step := range m.steps {
			if r, ok := resultMap[step.Name]; ok {
				if r.Success {
					m.steps[i].Status = "done"
				} else {
					m.steps[i].Status = "failed"
					m.steps[i].Error = r.Error
					if step.Required {
						hasFailure = true
					}
				}
			}
		}

		if hasFailure {
			m.hasError = true
			if m.parent != nil {
				m.parent.SetError("Environment setup failed. Press [R] to retry or [S] to skip optional checks.")
			}
		} else {
			m.hasError = false
			if m.parent != nil {
				m.parent.ErrorMsg = ""
				if m.parent.Session != nil {
					m.parent.Session.CurrentStage = session.StageEnvironmentReady
					if m.parent.SessionManager != nil {
						_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
					}
				}
				m.parent.SetStage(session.StageStack)
			}
		}

	case tea.KeyMsg:
		switch {
		case (msg.String() == "r" || msg.String() == "R") && m.hasError:
			cmds = append(cmds, m.RunAutomation())
		case (msg.String() == "s" || msg.String() == "S") && m.hasError:
			// Skip to next stage
			m.isSkipped = true
			m.hasError = false
			if m.parent != nil {
				m.parent.ErrorMsg = ""
				if m.parent.Session != nil {
					m.parent.Session.CurrentStage = session.StageEnvironmentReady
					if m.parent.SessionManager != nil {
						_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
					}
				}
				m.parent.SetStage(session.StageStack)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the environment automation stage screen.
func (m *EnvModel) View() string {
	title := TitleStyle.Render("✦ Environment & IDE Setup")
	subtitle := SubduedStyle.Render("Configuring Cursor MCP, VPN profile, and local development environment.")

	var stepRows []string
	for _, step := range m.steps {
		var statusIcon string
		var nameStyle lipgloss.Style

		switch step.Status {
		case "done":
			statusIcon = SuccessStyle.Render("✔")
			nameStyle = lipgloss.NewStyle().Foreground(ColorGreen)
		case "running":
			statusIcon = m.spinner.View()
			nameStyle = lipgloss.NewStyle().Foreground(ColorCyan)
		case "failed":
			statusIcon = ErrorStyle.Render("✖")
			nameStyle = lipgloss.NewStyle().Foreground(ColorRed)
		case "skipped":
			statusIcon = SubduedStyle.Render("—")
			nameStyle = lipgloss.NewStyle().Foreground(ColorSubdued)
		default: // pending
			statusIcon = SubduedStyle.Render("○")
			nameStyle = lipgloss.NewStyle().Foreground(ColorSubdued)
		}

		reqBadge := ""
		if step.Required {
			reqBadge = lipgloss.NewStyle().Foreground(ColorPurple).Render(" (required)")
		}

		row := fmt.Sprintf("  %s  %s%s", statusIcon, nameStyle.Render(step.Name), reqBadge)
		if step.Status == "failed" && step.Error != "" {
			row += "\n      " + ErrorStyle.Render(step.Error)
		}
		stepRows = append(stepRows, row)
	}

	var hintRow string
	if m.running {
		hintRow = SubduedStyle.Render(fmt.Sprintf("%s Running environment checks...", m.spinner.View()))
	} else if m.hasError {
		hintRow = lipgloss.JoinHorizontal(lipgloss.Left,
			KeyStyle.Render("[R]"), " ", KeyDescStyle.Render("Retry   "),
			KeyStyle.Render("[S]"), " ", KeyDescStyle.Render("Skip optional checks"),
		)
	} else if m.isSkipped {
		hintRow = SubduedStyle.Render("✓ Skipped optional environment checks.")
	}

	contentSection := lipgloss.JoinVertical(
		lipgloss.Left,
		strings.Join(stepRows, "\n"),
		"",
		hintRow,
	)

	w := m.width
	if m.parent != nil && m.parent.Width > 0 {
		w = m.parent.Width
	}

	cardWidth := 74
	if w > 20 {
		cardWidth = w - 8
		if cardWidth > 84 {
			cardWidth = 84
		}
	}

	box := CardBoxStyle.
		Width(cardWidth).
		Render(lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"",
			subtitle,
			"",
			contentSection,
		))

	if w > 0 {
		return lipgloss.NewStyle().
			Width(w).
			Align(lipgloss.Center).
			Render(box)
	}
	return box
}
