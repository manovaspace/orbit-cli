package onboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

// Orbit Brand Colors
var (
	ColorPurple   = lipgloss.Color("#7D56F4")
	ColorCyan     = lipgloss.Color("#00D2FF")
	ColorGreen    = lipgloss.Color("#00FF9D")
	ColorOrange   = lipgloss.Color("#FF9900")
	ColorGray     = lipgloss.Color("#626262")
	ColorDarkGray = lipgloss.Color("#2A2A2A")
	ColorBgDark   = lipgloss.Color("#121212")
	ColorWhite    = lipgloss.Color("#FAFAFA")
	ColorSubdued  = lipgloss.Color("#8E8E93")
	ColorRed      = lipgloss.Color("#FF5F56")
	ColorYellow   = lipgloss.Color("#FFBD2E")
)

// Base Typography and Layout Styles
var (
	// Brand & Headers
	BrandTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPurple)

	BrandSubtitleStyle = lipgloss.NewStyle().
				Foreground(ColorSubdued)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorCyan)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorSubdued)

	// Borders and Containers
	CardBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDarkGray).
			Padding(1, 2)

	ActiveCardBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorPurple).
				Padding(1, 2)

	HeaderBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDarkGray).
			Padding(0, 1)

	// Status & Badges
	SuccessStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGreen)

	ErrorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorRed)

	WarningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorYellow)

	InfoStyle = lipgloss.NewStyle().
			Foreground(ColorCyan)

	SubduedStyle = lipgloss.NewStyle().
			Foreground(ColorSubdued)

	// Key hints
	KeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPurple)

	KeyDescStyle = lipgloss.NewStyle().
			Foreground(ColorSubdued)

	// Stepper styles
	StepCompletedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorGreen)

	StepActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorCyan)

	StepPendingStyle = lipgloss.NewStyle().
				Foreground(ColorGray)

	StepSeparatorStyle = lipgloss.NewStyle().
				Foreground(ColorDarkGray)
)

// StepInfo represents a discrete onboarding wizard milestone step.
type StepInfo struct {
	Index int
	Title string
	Stage session.Stage
}

// WizardSteps defines the 6 primary stages of the onboarding pipeline.
var WizardSteps = []StepInfo{
	{Index: 1, Title: "Welcome", Stage: session.StageWelcome},
	{Index: 2, Title: "Pre-flight", Stage: session.StageDoctor},
	{Index: 3, Title: "Identity", Stage: session.StageIdentity},
	{Index: 4, Title: "Workspaces", Stage: session.StageWorkspace},
	{Index: 5, Title: "Environment", Stage: session.StageEnvironment},
	{Index: 6, Title: "Dev Stack", Stage: session.StageStack},
}

// StepIndexForStage maps any session.Stage to a 0-indexed primary wizard step (0 to 5).
func StepIndexForStage(st session.Stage) int {
	switch st {
	case session.StageInit, session.StageWelcome:
		return 0
	case session.StageDoctor, session.StageDoctorPassed:
		return 1
	case session.StageIdentity, session.StageKeypairReady, session.StageTokenClaimed, session.StageNetworkConfigured:
		return 2
	case session.StageWorkspace, session.StageReposCloned:
		return 3
	case session.StageEnvironment, session.StageEnvironmentReady, session.StageMCPConfigured:
		return 4
	case session.StageStack, session.StageStackReady, session.StageDevStackReady:
		return 5
	case session.StageComplete:
		return 6 // all completed
	default:
		return 0
	}
}

// RenderHeaderStepper generates the top horizontal progress stepper bar.
func RenderHeaderStepper(activeStage session.Stage, termWidth int) string {
	currentStepIdx := StepIndexForStage(activeStage)
	isFullyComplete := (activeStage == session.StageComplete || activeStage == session.StageCompleted)

	var stepStrings []string
	isCompact := termWidth > 0 && termWidth < 120

	for i, step := range WizardSteps {
		var stepLabel string
		var style lipgloss.Style

		if isFullyComplete || i < currentStepIdx {
			// Completed step
			checkIcon := "✔"
			if isCompact {
				stepLabel = fmt.Sprintf("%s %s", checkIcon, step.Title)
			} else {
				stepLabel = fmt.Sprintf("%s %d. %s", checkIcon, step.Index, step.Title)
			}
			style = StepCompletedStyle
		} else if i == currentStepIdx {
			// Active step
			activeIcon := "●"
			if isCompact {
				stepLabel = fmt.Sprintf("%s %s", activeIcon, step.Title)
			} else {
				stepLabel = fmt.Sprintf("%s %d. %s", activeIcon, step.Index, step.Title)
			}
			style = StepActiveStyle
		} else {
			// Pending step
			pendingIcon := "○"
			if isCompact {
				stepLabel = fmt.Sprintf("%s %s", pendingIcon, step.Title)
			} else {
				stepLabel = fmt.Sprintf("%s %d. %s", pendingIcon, step.Index, step.Title)
			}
			style = StepPendingStyle
		}

		stepStrings = append(stepStrings, style.Render(stepLabel))
	}

	sep := StepSeparatorStyle.Render(" ── ")
	if isCompact {
		sep = StepSeparatorStyle.Render(" › ")
	}

	joined := strings.Join(stepStrings, sep)
	if termWidth > 0 {
		return lipgloss.NewStyle().
			Width(termWidth).
			Align(lipgloss.Center).
			Render(joined)
	}
	return joined
}

// RenderFooterStatusBar generates the bottom key navigation hints bar.
func RenderFooterStatusBar(stage session.Stage, termWidth int, extraHints ...string) string {
	hints := []string{
		fmt.Sprintf("%s %s", KeyStyle.Render("[Enter]"), KeyDescStyle.Render("Advance")),
		fmt.Sprintf("%s %s", KeyStyle.Render("[Tab]"), KeyDescStyle.Render("Focus")),
	}

	for _, h := range extraHints {
		if strings.TrimSpace(h) != "" {
			hints = append(hints, h)
		}
	}

	hints = append(hints, fmt.Sprintf("%s %s", KeyStyle.Render("[Ctrl+C]"), KeyDescStyle.Render("Save & Exit")))

	barContent := strings.Join(hints, "   ")
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(ColorDarkGray).
		Foreground(ColorSubdued).
		Padding(0, 1)

	if termWidth > 0 {
		box = box.Width(termWidth)
	}

	return box.Render(barContent)
}

// RenderTooSmallWarning displays an informative screen when the terminal is under minimum dimensions.
func RenderTooSmallWarning(width, height int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorOrange).
		Padding(1, 3).
		Align(lipgloss.Center)

	title := BrandTitleStyle.Render("Orbit Onboarding Wizard")
	msg := fmt.Sprintf(
		"Terminal viewport too small: %dx%d\nPlease resize your terminal window to at least %dx%d.",
		width, height, 80, 24,
	)

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", SubduedStyle.Render(msg))
	return box.Render(content)
}
