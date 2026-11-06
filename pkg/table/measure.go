package table

import (
	"charm.land/lipgloss/v2"
)

func StringWidth(s string) int {
	return lipgloss.Width(s)
}
