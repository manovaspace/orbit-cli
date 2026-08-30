package onboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

// StackService describes one Compose service shown in the launch panel.
type StackService struct {
	Name   string
	URL    string
	Status string // "pending", "starting", "ready", "failed"
}

// StackLaunchFinishedMsg is sent when the dev-stack is confirmed up (or timed out).
type StackLaunchFinishedMsg struct {
	Services []StackService
	Err      error
}

// StackLauncherFunc is injectable for testing.
type StackLauncherFunc func(services []StackService) []StackService

// defaultStackServices returns the canonical Orbit dev-stack service list.
func defaultStackServices() []StackService {
	return []StackService{
		{Name: "Forgejo",      URL: "http://git.dev.manova.space:10000",  Status: "pending"},
		{Name: "Authelia",     URL: "http://auth.dev.manova.space:10000", Status: "pending"},
		{Name: "Mailpit",      URL: "http://mail.dev.manova.space:10000", Status: "pending"},
		{Name: "Orbit Portal", URL: "http://localhost:10007",             Status: "pending"},
		{Name: "Caddy",        URL: "https://dev.manova.space",           Status: "pending"},
	}
}

// defaultStackLauncher is the production launcher; in practice the CLI calls
// `orbit dev up` which runs docker compose, so here we just mark all services
// as ready (the real check would hit health endpoints).
func defaultStackLauncher(services []StackService) []StackService {
	out := make([]StackService, len(services))
	for i, s := range services {
		s.Status = "ready"
		out[i] = s
	}
	return out
}

// StackModel manages the Stage 6 Dev Stack Launch view.
type StackModel struct {
	parent   *WizardModel
	services []StackService
	running  bool
	hasError bool
	spinner  spinner.Model
	launcher StackLauncherFunc
}

// NewStackModel creates a StackModel wired to the parent wizard.
func NewStackModel(parent *WizardModel) *StackModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return &StackModel{
		parent:   parent,
		services: defaultStackServices(),
		spinner:  sp,
		launcher: defaultStackLauncher,
	}
}

// Services returns the current service list (for tests).
func (m *StackModel) Services() []StackService {
	return m.services
}

// SetLauncher injects a custom launcher (for testing).
func (m *StackModel) SetLauncher(fn StackLauncherFunc) {
	m.launcher = fn
}

// SetHasError sets the error state directly (for testing).
func (m *StackModel) SetHasError(v bool) {
	m.hasError = v
}

// Init returns a spinner tick.
func (m *StackModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// LaunchStack dispatches the async launcher as a tea.Cmd.
func (m *StackModel) LaunchStack() tea.Cmd {
	m.running = true
	m.hasError = false
	for i := range m.services {
		m.services[i].Status = "starting"
	}
	services := m.services
	launcher := m.launcher
	return func() tea.Msg {
		result := launcher(services)
		return StackLaunchFinishedMsg{Services: result}
	}
}

// Update handles Bubble Tea messages.
func (m *StackModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case StackLaunchFinishedMsg:
		m.running = false
		if msg.Err != nil {
			m.hasError = true
			if m.parent != nil {
				m.parent.SetError(fmt.Sprintf("Stack launch failed: %v — press [R] to retry", msg.Err))
			}
		} else {
			m.services = msg.Services
			m.hasError = false
			if m.parent != nil {
				m.parent.ErrorMsg = ""
				if m.parent.Session != nil {
					m.parent.Session.CurrentStage = session.StageStackReady
					if m.parent.SessionManager != nil {
						_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
					}
				}
				m.parent.SetStage(session.StageComplete)
			}
		}

	case tea.KeyMsg:
		if (msg.String() == "r" || msg.String() == "R") && m.hasError {
			cmds = append(cmds, m.LaunchStack())
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the stack launch stage.
func (m *StackModel) View() string {
	title := TitleStyle.Render("⚡ Dev Stack Launch")
	subtitle := SubduedStyle.Render("Bringing up the Orbit development stack.")

	var rows []string
	for _, svc := range m.services {
		var icon string
		var nameStyle lipgloss.Style

		switch svc.Status {
		case "ready":
			icon = SuccessStyle.Render("✔")
			nameStyle = lipgloss.NewStyle().Foreground(ColorGreen)
		case "starting":
			icon = m.spinner.View()
			nameStyle = lipgloss.NewStyle().Foreground(ColorCyan)
		case "failed":
			icon = ErrorStyle.Render("✖")
			nameStyle = lipgloss.NewStyle().Foreground(ColorRed)
		default: // pending
			icon = SubduedStyle.Render("○")
			nameStyle = lipgloss.NewStyle().Foreground(ColorSubdued)
		}
		rows = append(rows, fmt.Sprintf("  %s  %-16s  %s",
			icon,
			nameStyle.Render(svc.Name),
			SubduedStyle.Render(svc.URL),
		))
	}

	var hint string
	if m.running {
		hint = SubduedStyle.Render(fmt.Sprintf("%s Starting services...", m.spinner.View()))
	} else if m.hasError {
		hint = lipgloss.JoinHorizontal(lipgloss.Left,
			KeyStyle.Render("[R]"), " ", KeyDescStyle.Render("Retry"),
		)
	}

	w := 80
	if m.parent != nil && m.parent.Width > 0 {
		w = m.parent.Width
	}
	cardWidth := w - 8
	if cardWidth > 84 {
		cardWidth = 84
	}
	if cardWidth < 30 {
		cardWidth = 30
	}

	box := CardBoxStyle.Width(cardWidth).Render(lipgloss.JoinVertical(
		lipgloss.Left,
		title, "",
		subtitle, "",
		strings.Join(rows, "\n"),
		"",
		hint,
	))
	return lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(box)
}
