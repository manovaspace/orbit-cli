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
	// EndTruncate truncates an overlong flexible cell at the end ("abc…")
	// instead of middle-truncating values that contain a slash like a path.
	EndTruncate bool
	Align       Alignment
}

type Cell struct {
	Text   string
	Styled string
	Style  *lipgloss.Style
}

// Row is one table line. Detail, when set, prints on its own line directly
// below the row and is aligned with the table's detail column.
type Row struct {
	Cells  []Cell
	Detail string
}

func PlainCell(text string) Cell {
	return Cell{Text: text}
}

func StyledCell(plainText string, styledText string) Cell {
	return Cell{Text: plainText, Styled: styledText}
}

func StyleCell(text string, style lipgloss.Style) Cell {
	return Cell{Text: text, Style: &style}
}
