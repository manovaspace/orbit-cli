package table

import "github.com/charmbracelet/lipgloss"

type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
	AlignCenter
)

type Column struct {
	Title       string
	HeaderStyle lipgloss.Style
	CellStyle   lipgloss.Style
	MinWidth    int
	MaxWidth    int
	Flexible    bool
	Align       Alignment
}

type Cell struct {
	Text   string
	Styled string
	Style  *lipgloss.Style
}

type Row []Cell

func PlainCell(text string) Cell {
	return Cell{Text: text, Styled: text}
}

func StyledCell(plainText string, styledText string) Cell {
	return Cell{Text: plainText, Styled: styledText}
}
