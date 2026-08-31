package table

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestVisualWidthMeasurement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{name: "pure ascii", input: "orbit-cli", expected: 9},
		{name: "ansi bold", input: lipgloss.NewStyle().Bold(true).Render("orbit-cli"), expected: 9},
		{name: "ansi color and bold", input: lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true).Render("REPOSITORY"), expected: 10},
		{name: "unicode checkmark", input: "✔ clean", expected: 7},
		{name: "styled arrow status", input: lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("↑1 ahead"), expected: 8},
		{name: "empty string", input: "", expected: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StringWidth(tc.input)
			if got != tc.expected {
				t.Errorf("StringWidth(%q) = %d, want %d", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTableBuilderMethods(t *testing.T) {
	col1 := Column{Title: "REPO", Flexible: false}
	col2 := Column{Title: "PATH", Flexible: true}
	tbl := New(col1, col2)

	tbl.AddRow("orbit-cli", "orbit/orbit-cli")
	tbl.AddStyledRow(
		PlainCell("orbit-infra"),
		StyledCell("orbit/orbit-infra", lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("orbit/orbit-infra")),
	)
	tbl.AddStyledRow(
		PlainCell("status"),
		StyleCell("✔ active", lipgloss.NewStyle().Bold(true)),
	)

	if len(tbl.columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tbl.columns))
	}
	if len(tbl.rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(tbl.rows))
	}
}
