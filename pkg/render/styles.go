package render

import "github.com/charmbracelet/lipgloss"

// Styles shared by every Orbit command. They keep the colors the CLI has used
// since the original cmd/orbit/ui.go so phase 1 changes no existing output.
var (
	// Title is the bold title style without a margin. The legacy title carried
	// a bottom margin; TitleLegacy keeps it for the pre-ADR-046 call sites.
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12"))

	// TitleLegacy is Title with the historical bottom margin. Only the
	// cmd/orbit aliases use it; new code uses Title and prints its own blank
	// line through Printer.Title.
	TitleLegacy = Title.MarginBottom(1)

	// Header marks a section rule.
	Header = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14"))

	// Bold is unstyled text with weight only.
	Bold = lipgloss.NewStyle().
		Bold(true)

	// Success marks a successful result.
	Success = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10"))

	// Warning marks a non-fatal problem.
	Warning = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("11"))

	// Error marks a failure.
	Error = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("9"))

	// Info marks neutral information.
	Info = lipgloss.NewStyle().
		Foreground(lipgloss.Color("14"))

	// Subtle marks secondary text such as labels and hints.
	Subtle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	// Code marks a command, path or identifier.
	Code = lipgloss.NewStyle().
		Foreground(lipgloss.Color("13"))

	// Card draws a rounded border around a block of text.
	Card = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		MarginTop(1)
)

// Pre-rendered status icons. New code uses Status.Icon instead; these exist so
// cmd/orbit/ui.go can keep its unexported aliases unchanged.
var (
	IconOK    = Success.Render("✔")
	IconWarn  = Warning.Render("⚠")
	IconError = Error.Render("✖")
	IconInfo  = Info.Render("ℹ")
	IconArrow = Subtle.Render("→")
)
