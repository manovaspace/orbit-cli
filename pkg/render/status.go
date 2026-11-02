package render

import "github.com/charmbracelet/lipgloss"

// Status is the outcome of one check or row. Unknown values render as info so
// a caller never prints an empty icon.
type Status int

const (
	StatusOK Status = iota
	StatusWarn
	StatusFail
	StatusInfo
	StatusSkipped
	StatusUnverified
)

// Icon returns the styled status glyph.
func (s Status) Icon() string {
	return s.Style().Render(s.glyph())
}

// Label returns the plain-text status name used by the plain fallback.
func (s Status) Label() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	case StatusInfo:
		return "info"
	case StatusSkipped:
		return "skipped"
	case StatusUnverified:
		return "unverified"
	default:
		return "info"
	}
}

// Style returns the lipgloss style for the status.
func (s Status) Style() lipgloss.Style {
	switch s {
	case StatusOK:
		return Success
	case StatusWarn, StatusUnverified:
		return Warning
	case StatusFail:
		return Error
	case StatusSkipped:
		return Subtle
	case StatusInfo:
		return Info
	default:
		return Info
	}
}

func (s Status) glyph() string {
	switch s {
	case StatusOK:
		return "✔"
	case StatusWarn:
		return "⚠"
	case StatusFail:
		return "✖"
	case StatusInfo:
		return "ℹ"
	case StatusSkipped:
		return "○"
	case StatusUnverified:
		return "?"
	default:
		return "ℹ"
	}
}
