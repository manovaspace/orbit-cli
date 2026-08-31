package onboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

const welcomeBannerASCII = `   ___      _     _ _   
  / _ \ _ _| |__ (_) |_ 
 | (_) | '_| '_ \| |  _|
  \___/|_| |_.__/|_|\__|`

// WelcomeModel manages the Stage 1 Welcome and Invitation Token entry screen.
type WelcomeModel struct {
	parent            *WizardModel
	tokenInput        textinput.Model
	hasPendingSession bool
	pendingSession    *session.SessionState
	width             int
}

// NewWelcomeModel initializes a new WelcomeModel attached to the root WizardModel.
func NewWelcomeModel(parent *WizardModel) *WelcomeModel {
	ti := textinput.New()
	ti.Placeholder = "Paste your invitation token (e.g. inv_... or orb_...)"
	ti.CharLimit = 128
	ti.Width = 56
	ti.Focus()
	ti.Prompt = "Token ❯ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(ColorCyan)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(ColorGray)

	var hasPending bool
	var pendingSess *session.SessionState

	if parent != nil && parent.SessionManager != nil {
		if parent.SessionManager.HasPendingSession() && !parent.Options.Resume && !parent.Options.Reset {
			loaded, err := parent.SessionManager.LoadSession()
			if err == nil && loaded != nil && loaded.CurrentStage != session.StageComplete && loaded.CurrentStage != session.StageCompleted && loaded.CurrentStage != session.StageInit && loaded.CurrentStage != session.StageWelcome {
				hasPending = true
				pendingSess = loaded
			}
		}
	}

	wm := &WelcomeModel{
		parent:            parent,
		tokenInput:        ti,
		hasPendingSession: hasPending,
		pendingSession:    pendingSess,
		width:             MinTerminalWidth,
	}

	if parent != nil {
		if parent.Options.PreSetToken != "" {
			wm.SetTokenValue(parent.Options.PreSetToken)
		} else if parent.Session != nil && parent.Session.InviteToken != "" {
			wm.SetTokenValue(parent.Session.InviteToken)
		}
		if parent.Width > 0 {
			wm.width = parent.Width
		}
	}

	return wm
}

// Init activates the text input cursor.
func (m *WelcomeModel) Init() tea.Cmd {
	return m.tokenInput.Focus()
}

// TokenValue returns the trimmed value of the invitation token input field.
func (m *WelcomeModel) TokenValue() string {
	return strings.TrimSpace(m.tokenInput.Value())
}

// SetTokenValue pre-fills the invitation token input field.
func (m *WelcomeModel) SetTokenValue(tok string) {
	m.tokenInput.SetValue(strings.TrimSpace(tok))
}

// Update handles key presses and user interaction for the welcome stage.
func (m *WelcomeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		if m.hasPendingSession {
			switch msg.String() {
			case "r", "R", "enter":
				if m.parent != nil && m.pendingSession != nil {
					m.parent.Session = m.pendingSession
					targetStage := m.pendingSession.CurrentStage
					if targetStage == "" || targetStage == session.StageInit {
						targetStage = session.StageDoctor
					}
					m.parent.SetStage(targetStage)
				}
				m.hasPendingSession = false
				return m, nil

			case "d", "D":
				if m.parent != nil && m.parent.SessionManager != nil {
					_ = m.parent.SessionManager.DiscardSession()
					m.parent.Session = m.parent.SessionManager.CreateSession("", "")
				}
				m.hasPendingSession = false
				m.tokenInput.Focus()
				return m, nil
			}
			return m, nil
		}

		// Token input mode
		switch msg.Type {
		case tea.KeyEnter:
			tok := m.TokenValue()
			if tok == "" {
				if m.parent != nil {
					m.parent.SetError("Invitation token cannot be empty. Please paste your token.")
				}
				return m, nil
			}

			if m.parent != nil {
				m.parent.ErrorMsg = ""
				if m.parent.Session != nil {
					m.parent.Session.InviteToken = tok
					if m.parent.SessionManager != nil {
						_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
					}
				}
				m.parent.SetStage(session.StageDoctor)
			}
			return m, nil
		}
	}

	if !m.hasPendingSession {
		var cmd tea.Cmd
		m.tokenInput, cmd = m.tokenInput.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the Welcome and Token Entry screen.
func (m *WelcomeModel) View() string {
	banner := BrandTitleStyle.Render(welcomeBannerASCII)
	subtitle := SubtitleStyle.Render("Welcome to Manova. Fast, resilient engineering onboarding in under 3 minutes.")

	var contentSection string

	if m.hasPendingSession && m.pendingSession != nil {
		promptTitle := WarningStyle.Render("⚡ Saved checkpoint found")
		stageName := string(m.pendingSession.CurrentStage)
		promptDesc := lipgloss.NewStyle().Foreground(ColorWhite).Render(
			fmt.Sprintf("An incomplete onboarding session was detected (Stage: %s).", StepActiveStyle.Render(stageName)),
		)

		resumeBtn := ActiveCardBoxStyle.Render(fmt.Sprintf("%s Resume incomplete session", KeyStyle.Render("[R]")))
		discardBtn := CardBoxStyle.Render(fmt.Sprintf("%s Discard and start fresh", KeyStyle.Render("[D]")))
		actions := lipgloss.JoinHorizontal(lipgloss.Center, resumeBtn, "   ", discardBtn)

		hint := SubduedStyle.Render("Press [R] or [Enter] to resume saved session, or [D] to discard and start fresh.")

		contentSection = lipgloss.JoinVertical(
			lipgloss.Center,
			promptTitle,
			"",
			promptDesc,
			"",
			actions,
			"",
			hint,
		)
	} else {
		inputTitle := TitleStyle.Render("Enter Invitation Token")
		inputDesc := SubduedStyle.Render("Paste your engineering onboarding invitation or claim token to begin.")

		inputField := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPurple).
			Padding(0, 1).
			Render(m.tokenInput.View())

		tokenHint := SubduedStyle.Render("💡 Tokens typically begin with inv_ (e.g. inv_dev_...) or orb_")

		contentSection = lipgloss.JoinVertical(
			lipgloss.Left,
			inputTitle,
			"",
			inputDesc,
			"",
			inputField,
			"",
			tokenHint,
		)
	}

	w := m.width
	if m.parent != nil && m.parent.Width > 0 {
		w = m.parent.Width
	}

	cardWidth := 72
	if w > 20 {
		cardWidth = w - 8
		if cardWidth > 76 {
			cardWidth = 76
		}
	}

	box := CardBoxStyle.
		Width(cardWidth).
		Render(lipgloss.JoinVertical(
			lipgloss.Center,
			banner,
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
