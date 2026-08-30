package onboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

// MinTerminalWidth is the minimum terminal width required for full onboarding TUI.
const MinTerminalWidth = 80

// MinTerminalHeight is the minimum terminal height required for full onboarding TUI.
const MinTerminalHeight = 24

// WizardOptions encapsulates initialization parameters for the Onboarding TUI wizard.
type WizardOptions struct {
	SessionManager *session.SessionManager
	PreSetToken    string
	Resume         bool
	Reset          bool
	Rollback       bool
	NonInteractive bool
}

// WizardModel is the root Bubble Tea model managing the interactive onboarding lifecycle.
type WizardModel struct {
	Stage          session.Stage
	Session        *session.SessionState
	SessionManager *session.SessionManager
	Options        WizardOptions

	Width        int
	Height       int
	TermTooSmall bool

	Spinner    spinner.Model
	IsLoading  bool
	LoadingMsg string
	ErrorMsg   string
	SuccessMsg string

	// Active stage view handler overrides (can be wired by individual stage components)
	StageViews   map[session.Stage]func(m *WizardModel) string
	StageUpdates map[session.Stage]func(m *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd)
}

// NewWizardModel creates and initializes a new root onboarding wizard model.
func NewWizardModel(opts WizardOptions) *WizardModel {
	sm := opts.SessionManager
	if sm == nil {
		var err error
		sm, err = session.NewSessionManager("")
		if err != nil {
			// Fallback session manager if home directory cannot be determined
			sm, _ = session.NewSessionManager("/tmp/orbit-session.json")
		}
	}

	var sess *session.SessionState
	if opts.Reset || opts.Rollback {
		_ = sm.DiscardSession()
		sess = sm.CreateSession("", "")
	} else if opts.Resume || sm.HasPendingSession() {
		loaded, err := sm.LoadSession()
		if err == nil && loaded != nil {
			sess = loaded
		}
	}

	if sess == nil {
		sess = sm.CreateSession("", "")
	}

	if opts.PreSetToken != "" {
		sess.InviteToken = opts.PreSetToken
		sess.ClaimToken = opts.PreSetToken
	}

	// Determine starting stage
	startStage := sess.CurrentStage
	if !opts.Resume || startStage == "" || startStage == session.StageInit {
		startStage = session.StageWelcome
		sess.CurrentStage = startStage
	}

	// Initialize spinner
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorPurple)

	m := &WizardModel{
		Stage:          startStage,
		Session:        sess,
		SessionManager: sm,
		Options:        opts,
		Width:          MinTerminalWidth,
		Height:         MinTerminalHeight,
		Spinner:        sp,
		StageViews:     make(map[session.Stage]func(m *WizardModel) string),
		StageUpdates:   make(map[session.Stage]func(m *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd)),
	}

	// Wire Welcome stage component
	welcomeModel := NewWelcomeModel(m)
	m.StageViews[session.StageWelcome] = func(w *WizardModel) string {
		return welcomeModel.View()
	}
	m.StageUpdates[session.StageWelcome] = func(w *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd) {
		_, cmd := welcomeModel.Update(msg)
		return w, cmd
	}

	// Wire Doctor stage component
	doctorModel := NewDoctorModel(m)
	m.StageViews[session.StageDoctor] = func(w *WizardModel) string {
		return doctorModel.View()
	}
	m.StageUpdates[session.StageDoctor] = func(w *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd) {
		_, cmd := doctorModel.Update(msg)
		return w, cmd
	}

	// Wire Identity stage component
	identityModel := NewIdentityModel(m)
	m.StageViews[session.StageIdentity] = func(w *WizardModel) string {
		return identityModel.View()
	}
	m.StageUpdates[session.StageIdentity] = func(w *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd) {
		_, cmd := identityModel.Update(msg)
		return w, cmd
	}

	return m
}

// Init initializes the Bubble Tea program with the spinner tick command.
func (m *WizardModel) Init() tea.Cmd {
	return tea.Batch(m.Spinner.Tick, textinput.Blink)
}

// SetStage transitions the wizard to a new stage and persists a checkpoint.
func (m *WizardModel) SetStage(stage session.Stage) {
	m.Stage = stage
	m.ErrorMsg = ""
	if m.Session != nil {
		m.Session.CurrentStage = stage
		if m.SessionManager != nil {
			_ = m.SessionManager.SaveCheckpoint(m.Session)
		}
	}
}

// ActiveStage returns the currently active stage of the wizard.
func (m *WizardModel) ActiveStage() session.Stage {
	return m.Stage
}

// SetError updates the error banner message.
func (m *WizardModel) SetError(err string) {
	m.ErrorMsg = err
	m.IsLoading = false
}

// SetLoading updates the loading spinner state and message.
func (m *WizardModel) SetLoading(loading bool, msg string) {
	m.IsLoading = loading
	m.LoadingMsg = msg
}

// Update handles incoming Bubble Tea messages (keys, window resizing, spinners).
func (m *WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.TermTooSmall = (msg.Width < MinTerminalWidth || msg.Height < MinTerminalHeight)
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			// Persist checkpoint on abort
			if m.Session != nil && m.SessionManager != nil {
				_ = m.SessionManager.SaveCheckpoint(m.Session)
			}
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.Spinner, cmd = m.Spinner.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Delegate to active stage updater if registered
	if stageUpdater, ok := m.StageUpdates[m.Stage]; ok {
		newModel, cmd := stageUpdater(m, msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return newModel, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

// View renders the complete full-screen TUI interface.
func (m *WizardModel) View() string {
	if m.TermTooSmall {
		return RenderTooSmallWarning(m.Width, m.Height)
	}

	// Header section
	headerTitle := BrandTitleStyle.Render("✦ ORBIT") + " " + SubduedStyle.Render("· Developer Onboarding Wizard")
	stepper := RenderHeaderStepper(m.Stage, m.Width)

	headerBlock := lipgloss.JoinVertical(
		lipgloss.Center,
		"",
		headerTitle,
		"",
		stepper,
		"",
	)

	// Main content body
	var body string
	if customView, ok := m.StageViews[m.Stage]; ok {
		body = customView(m)
	} else {
		body = m.defaultStageView()
	}

	// Error banner if any
	if m.ErrorMsg != "" {
		errBox := ErrorStyle.Render("✖ Error: " + m.ErrorMsg)
		body = lipgloss.JoinVertical(lipgloss.Left, errBox, "", body)
	}

	// Loading spinner if active
	if m.IsLoading {
		spinnerLine := fmt.Sprintf("%s %s", m.Spinner.View(), InfoStyle.Render(m.LoadingMsg))
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", spinnerLine)
	}

	// Footer section
	footer := RenderFooterStatusBar(m.Stage, m.Width)

	// Calculate vertical spacing
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		headerBlock,
		body,
	)

	// Place footer at the bottom if height is specified
	if m.Height > 0 {
		contentHeight := lipgloss.Height(content)
		footerHeight := lipgloss.Height(footer)
		padding := m.Height - contentHeight - footerHeight
		if padding > 0 {
			content = lipgloss.JoinVertical(
				lipgloss.Left,
				content,
				strings.Repeat("\n", padding-1),
				footer,
			)
			return content
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, content, "", footer)
}

// defaultStageView returns a default formatted card when stage-specific view is initializing.
func (m *WizardModel) defaultStageView() string {
	stageTitle := TitleStyle.Render(fmt.Sprintf("Stage: %s", m.Stage))
	stageDesc := SubduedStyle.Render("Initializing onboarding stage components...")

	box := CardBoxStyle.
		Width(m.Width - 4).
		Render(lipgloss.JoinVertical(lipgloss.Left, stageTitle, "", stageDesc))

	return box
}

// RunWizard runs the Bubble Tea onboarding program in the terminal.
func RunWizard(opts WizardOptions) error {
	model := NewWizardModel(opts)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
