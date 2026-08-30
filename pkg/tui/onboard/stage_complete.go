package onboard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

// DashboardInfo holds the service URLs for the completion screen.
type DashboardInfo struct {
	PortalURL  string
	AuthURL    string
	MailpitURL string
	GitURL     string
	TotalRepos int
}

// RenderCompletionDashboard renders the final completion card as a plain string.
// Used directly by tests (no Bubble Tea model needed).
func RenderCompletionDashboard(info DashboardInfo) string {
	title := TitleStyle.Render("✦ You're all set!")
	subtitle := SubduedStyle.Render("The Orbit development environment is fully configured.")

	linkStyle := lipgloss.NewStyle().Foreground(ColorCyan).Underline(true)
	labelStyle := lipgloss.NewStyle().Foreground(ColorSubdued).Width(16)

	links := []struct{ label, url string }{
		{"Orbit Portal", info.PortalURL},
		{"Authelia Auth", info.AuthURL},
		{"Mailpit", info.MailpitURL},
		{"Forgejo Git", info.GitURL},
	}

	var rows []string
	for _, l := range links {
		if l.url == "" {
			continue
		}
		rows = append(rows, fmt.Sprintf("  %s  %s",
			labelStyle.Render(l.label),
			linkStyle.Render(l.url),
		))
	}

	repoLine := ""
	if info.TotalRepos > 0 {
		repoLine = SuccessStyle.Render(fmt.Sprintf("✔ %d workspace repositories cloned", info.TotalRepos))
	}

	nextSteps := lipgloss.JoinVertical(
		lipgloss.Left,
		SubduedStyle.Render("Next steps:"),
		"  "+KeyStyle.Render("orbit dev up")+"  "+KeyDescStyle.Render("Start the dev stack"),
		"  "+KeyStyle.Render("orbit staff ls")+"  "+KeyDescStyle.Render("List staff members"),
		"  "+KeyStyle.Render("orbit config ls")+"  "+KeyDescStyle.Render("Review your config"),
	)

	sections := []string{title, "", subtitle, ""}
	if repoLine != "" {
		sections = append(sections, repoLine, "")
	}
	sections = append(sections, strings.Join(rows, "\n"), "", nextSteps)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// CompleteModel manages the final completion dashboard stage.
type CompleteModel struct {
	parent    *WizardModel
	dashboard string
}

// NewCompleteModel creates a CompleteModel for the completion stage.
func NewCompleteModel(parent *WizardModel) *CompleteModel {
	return &CompleteModel{parent: parent}
}

// buildDashboard assembles DashboardInfo from the parent session and renders.
func (m *CompleteModel) buildDashboard() string {
	info := DashboardInfo{
		PortalURL:  "http://localhost:10007",
		AuthURL:    "http://auth.dev.manova.space:10000",
		MailpitURL: "http://mail.dev.manova.space:10000",
		GitURL:     "http://git.dev.manova.space:10000",
	}

	if m.parent != nil && m.parent.Session != nil {
		info.TotalRepos = len(m.parent.Session.ClonedRepos)
	}

	return RenderCompletionDashboard(info)
}

// Init completes the session on first view.
func (m *CompleteModel) Init() tea.Cmd {
	return func() tea.Msg {
		return completeSessionMsg{}
	}
}

type completeSessionMsg struct{}

// Update handles messages for the completion stage.
func (m *CompleteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case completeSessionMsg:
		if m.parent != nil {
			if m.parent.Session != nil {
				m.parent.Session.CurrentStage = session.StageCompleted
				if m.parent.SessionManager != nil {
					_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
				}
			}
			m.parent.ErrorMsg = ""
		}
		m.dashboard = m.buildDashboard()

	case tea.KeyMsg:
		// Allow q/Ctrl-C to quit from the done screen
		switch msg.(tea.KeyMsg).String() {
		case "q", "Q", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, nil
}

// View renders the completion stage.
func (m *CompleteModel) View() string {
	content := m.dashboard
	if content == "" {
		content = m.buildDashboard()
	}

	hint := lipgloss.JoinHorizontal(
		lipgloss.Left,
		KeyStyle.Render("[Q]"), " ", KeyDescStyle.Render("Quit wizard"),
	)

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
		content,
		"",
		hint,
	))
	return lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(box)
}
