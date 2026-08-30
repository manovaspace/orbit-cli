package onboard

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/doctor/healer"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

// DiagnosticsFinishedMsg is sent when background pre-flight diagnostics complete.
type DiagnosticsFinishedMsg struct {
	Report *doctor.DoctorReport
	Err    error
}

// HealProgressMsg is sent during incremental auto-healing execution.
type HealProgressMsg struct {
	HealerName string
	Step       string
}

// HealFinishedMsg is sent when all auto-healing recipes finish running.
type HealFinishedMsg struct {
	Results []healer.HealResult
	Err     error
}

// DiagnosticsRunnerFunc defines a signature for executing diagnostics.
type DiagnosticsRunnerFunc func() *doctor.DoctorReport

// HealerRunnerFunc defines a signature for executing auto-healing recipes.
type HealerRunnerFunc func(ctx context.Context, results []doctor.DiagnosticResult, progress func(string, string)) ([]healer.HealResult, error)

// DoctorModel manages the Stage 2 Pre-flight Diagnostics and Auto-healing view.
type DoctorModel struct {
	parent       *WizardModel
	report       *doctor.DoctorReport
	running      bool
	healing      bool
	healProgress string
	healResults  []healer.HealResult
	width        int
	spinner      spinner.Model

	customRunner DiagnosticsRunnerFunc
	customHealer HealerRunnerFunc
}

// NewDoctorModel initializes a new DoctorModel attached to the root WizardModel.
func NewDoctorModel(parent *WizardModel) *DoctorModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorPurple)

	w := MinTerminalWidth
	if parent != nil && parent.Width > 0 {
		w = parent.Width
	}

	return &DoctorModel{
		parent:  parent,
		spinner: sp,
		width:   w,
	}
}

// Init starts the spinner tick and triggers async diagnostics if no report is present.
func (m *DoctorModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, m.spinner.Tick)
	if m.report == nil && !m.running {
		cmds = append(cmds, m.RunDiagnostics())
	}
	return tea.Batch(cmds...)
}

// SetReport assigns a pre-computed diagnostic report and clears the running state.
func (m *DoctorModel) SetReport(report *doctor.DoctorReport) {
	m.report = report
	m.running = false
	m.healing = false
	if report != nil {
		for i := range report.Results {
			if report.Results[i].Status != doctor.StatusOK && healer.IsAutoHealable(report.Results[i]) {
				report.Results[i].IsHealable = true
			}
		}
	}
}

// Report returns the current diagnostic report.
func (m *DoctorModel) Report() *doctor.DoctorReport {
	return m.report
}

// IsRunning returns true if diagnostics are currently executing.
func (m *DoctorModel) IsRunning() bool {
	return m.running
}

// IsHealing returns true if auto-healing recipes are currently executing.
func (m *DoctorModel) IsHealing() bool {
	return m.healing
}

// SetDiagnosticsRunner overrides the diagnostics runner (useful for testing).
func (m *DoctorModel) SetDiagnosticsRunner(fn DiagnosticsRunnerFunc) {
	m.customRunner = fn
}

// SetHealerRunner overrides the auto-healer runner (useful for testing).
func (m *DoctorModel) SetHealerRunner(fn HealerRunnerFunc) {
	m.customHealer = fn
}

// RunDiagnostics dispatches an asynchronous command to execute pre-flight diagnostics.
func (m *DoctorModel) RunDiagnostics() tea.Cmd {
	m.running = true
	m.healing = false
	return func() tea.Msg {
		var rep *doctor.DoctorReport
		if m.customRunner != nil {
			rep = m.customRunner()
		} else {
			rep = doctor.RunDiagnostics()
		}

		if rep != nil {
			for i := range rep.Results {
				if rep.Results[i].Status != doctor.StatusOK && healer.IsAutoHealable(rep.Results[i]) {
					rep.Results[i].IsHealable = true
				}
			}
		}

		return DiagnosticsFinishedMsg{
			Report: rep,
		}
	}
}

// RunAutoHeal dispatches an asynchronous command to execute auto-healing recipes.
func (m *DoctorModel) RunAutoHeal() tea.Cmd {
	m.healing = true
	m.running = false
	m.healProgress = "Starting auto-healing..."
	healables := m.healableResults()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		var results []healer.HealResult
		var err error

		if m.customHealer != nil {
			results, err = m.customHealer(ctx, healables, func(name, step string) {})
		} else {
			results, err = healer.RunHealers(ctx, healables, func(name, step string) {})
		}

		return HealFinishedMsg{
			Results: results,
			Err:     err,
		}
	}
}

func (m *DoctorModel) healableResults() []doctor.DiagnosticResult {
	if m.report == nil {
		return nil
	}
	var out []doctor.DiagnosticResult
	for _, res := range m.report.Results {
		if res.Status != doctor.StatusOK && (res.IsHealable || healer.IsAutoHealable(res)) {
			out = append(out, res)
		}
	}
	return out
}

// Update processes Bubble Tea events, messages, and keybindings for the Doctor stage.
func (m *DoctorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		// If first time arriving at StageDoctor and no report is present, automatically trigger diagnostics scan
		if m.report == nil && !m.running && !m.healing {
			cmds = append(cmds, m.RunDiagnostics())
		}

	case DiagnosticsFinishedMsg:
		m.running = false
		m.report = msg.Report
		if m.parent != nil {
			if msg.Err != nil {
				m.parent.SetError(fmt.Sprintf("Diagnostics scan failed: %v", msg.Err))
			} else {
				m.parent.ErrorMsg = ""
			}
		}
		return m, nil

	case HealFinishedMsg:
		m.healing = false
		m.healResults = msg.Results
		if m.parent != nil {
			if msg.Err != nil {
				m.parent.SetError(fmt.Sprintf("Auto-heal encountered an error: %v", msg.Err))
			} else {
				m.parent.ErrorMsg = ""
			}
		}
		// Refresh diagnostics after healing
		return m, m.RunDiagnostics()

	case tea.KeyMsg:
		switch msg.String() {
		case "h", "H", "f", "F":
			if !m.running && !m.healing {
				healables := m.healableResults()
				if len(healables) > 0 {
					return m, m.RunAutoHeal()
				}
			}
			return m, nil

		case "r", "R":
			if !m.running && !m.healing {
				return m, m.RunDiagnostics()
			}
			return m, nil

		case "enter":
			if m.running || m.healing {
				return m, nil
			}

			if m.report == nil {
				return m, m.RunDiagnostics()
			}

			if m.report.HasErrors() {
				healables := m.healableResults()
				if len(healables) > 0 {
					if m.parent != nil {
						m.parent.SetError("Auto-fix is available for unresolved issues. Press [H] to auto-heal or [R] to retry.")
					}
				} else {
					if m.parent != nil {
						m.parent.SetError("Pre-flight errors detected. Please fix the required dependencies above and press [R] to retry.")
					}
				}
				return m, nil
			}

			// All required diagnostics passed (warnings allowed)
			if m.parent != nil {
				m.parent.ErrorMsg = ""
				if m.parent.Session != nil {
					m.parent.Session.CurrentStage = session.StageDoctorPassed
					if m.parent.SessionManager != nil {
						_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
					}
				}
				m.parent.SetStage(session.StageIdentity)
			}
			return m, nil
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the Doctor diagnostic checks table, category groups, and auto-heal cards.
func (m *DoctorModel) View() string {
	title := TitleStyle.Render("✦ Pre-Flight System Diagnostics")
	subtitle := SubduedStyle.Render("Validating host architecture, compiler toolchains, runtimes, and local dev stack.")

	var contentSection string

	if m.running {
		contentSection = lipgloss.JoinVertical(
			lipgloss.Center,
			"",
			fmt.Sprintf("%s %s", m.spinner.View(), InfoStyle.Render("Running pre-flight system diagnostics...")),
			"",
			SubduedStyle.Render("Scanning host OS, Go compiler, Bun, Docker daemon, and dev ports..."),
			"",
		)
	} else if m.healing {
		progressText := m.healProgress
		if progressText == "" {
			progressText = "Applying automated toolchain fixes..."
		}
		contentSection = lipgloss.JoinVertical(
			lipgloss.Center,
			"",
			fmt.Sprintf("%s %s", m.spinner.View(), WarningStyle.Render("Auto-healing toolchain dependencies...")),
			"",
			InfoStyle.Render(progressText),
			"",
		)
	} else if m.report != nil {
		var bodyBlocks []string

		// Category order
		categoryOrder := []string{
			doctor.CategoryCore,
			doctor.CategoryToolchain,
			doctor.CategoryRuntime,
			doctor.CategoryDevTools,
			doctor.CategoryContainer,
			doctor.CategoryNetwork,
			doctor.CategoryPorts,
			doctor.CategorySecurity,
			doctor.CategoryAuth,
			doctor.CategoryOptional,
			doctor.CategoryWorkspace,
		}

		// Group results by category
		grouped := make(map[string][]doctor.DiagnosticResult)
		var customCategories []string

		for _, res := range m.report.Results {
			cat := res.Category
			if cat == "" {
				cat = doctor.CategoryCore
			}
			if len(grouped[cat]) == 0 {
				isKnown := false
				for _, known := range categoryOrder {
					if known == cat {
						isKnown = true
						break
					}
				}
				if !isKnown {
					customCategories = append(customCategories, cat)
				}
			}
			grouped[cat] = append(grouped[cat], res)
		}

		allCategories := append(categoryOrder, customCategories...)

		for _, cat := range allCategories {
			items := grouped[cat]
			if len(items) == 0 {
				continue
			}

			catHeader := lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorPurple).
				Render("── " + cat + " ──────────────────────────────────────────")
			bodyBlocks = append(bodyBlocks, "", catHeader)

			for _, item := range items {
				var icon string
				var msgStyle lipgloss.Style

				switch item.Status {
				case doctor.StatusOK:
					icon = SuccessStyle.Render("✔")
					msgStyle = SubduedStyle
				case doctor.StatusWarning:
					icon = WarningStyle.Render("▲")
					msgStyle = WarningStyle
				case doctor.StatusError:
					icon = ErrorStyle.Render("✖")
					msgStyle = ErrorStyle
				default:
					icon = InfoStyle.Render("●")
					msgStyle = SubduedStyle
				}

				nameCol := lipgloss.NewStyle().
					Bold(true).
					Foreground(ColorWhite).
					Width(26).
					Render(item.Name)

				msgCol := msgStyle.Render(item.Message)

				line := fmt.Sprintf("  %s %s %s", icon, nameCol, msgCol)
				if item.Status != doctor.StatusOK && (item.IsHealable || healer.IsAutoHealable(item)) {
					line += " " + InfoStyle.Render("[Auto-fix available]")
				}
				bodyBlocks = append(bodyBlocks, line)
			}
		}

		// Summary banner
		passed := m.report.CountPassed()
		warnings := m.report.CountWarnings()
		errors := m.report.CountErrors()
		healables := len(m.healableResults())

		summaryRow := fmt.Sprintf("%s   %s   %s",
			SuccessStyle.Render(fmt.Sprintf("✔ %d passed", passed)),
			WarningStyle.Render(fmt.Sprintf("▲ %d warnings", warnings)),
			ErrorStyle.Render(fmt.Sprintf("✖ %d errors", errors)),
		)

		var actionCard string
		if errors > 0 {
			if healables > 0 {
				actionCard = ActiveCardBoxStyle.Render(lipgloss.JoinVertical(
					lipgloss.Left,
					WarningStyle.Render(fmt.Sprintf("⚡ Auto-fix available for %d issue(s).", healables)),
					lipgloss.NewStyle().Foreground(ColorWhite).Render(fmt.Sprintf("Press %s to auto-heal or %s to retry.", KeyStyle.Render("[H] Auto-heal"), KeyStyle.Render("[Enter]"))),
				))
			} else {
				actionCard = CardBoxStyle.Render(lipgloss.JoinVertical(
					lipgloss.Left,
					ErrorStyle.Render("✖ Critical pre-flight errors detected."),
					SubduedStyle.Render("Please address the missing dependencies above and press [R] or [Enter] to retry."),
				))
			}
		} else {
			actionCard = SuccessStyle.Render("✔ All required pre-flight diagnostics passed! Press [Enter] to continue.")
		}

		bodyBlocks = append(bodyBlocks, "", summaryRow, "", actionCard)
		contentSection = lipgloss.JoinVertical(lipgloss.Left, bodyBlocks...)
	} else {
		contentSection = SubduedStyle.Render("Initializing pre-flight diagnostic scan...")
	}

	w := m.width
	if m.parent != nil && m.parent.Width > 0 {
		w = m.parent.Width
	}

	cardWidth := 74
	if w > 20 {
		cardWidth = w - 8
		if cardWidth > 82 {
			cardWidth = 82
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
