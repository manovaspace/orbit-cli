package table

import (
	"github.com/charmbracelet/lipgloss"
)

func StringWidth(s string) int {
	return lipgloss.Width(s)
}
