package table

import (
	"github.com/charmbracelet/lipgloss"
)

type Table struct {
	columns      []Column
	rows         []Row
	termWidth    int
	padding      int
	indent       string
	dividerStyle lipgloss.Style
	showDivider  bool
	page         int
	limit        int
}

func New(columns ...Column) *Table {
	return &Table{
		columns:      columns,
		rows:         make([]Row, 0),
		padding:      2,
		indent:       "  ",
		dividerStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		showDivider:  true,
	}
}

func (t *Table) AddRow(values ...string) *Table {
	row := make(Row, len(values))
	for i, v := range values {
		row[i] = PlainCell(v)
	}
	t.rows = append(t.rows, row)
	return t
}

func (t *Table) AddStyledRow(cells ...Cell) *Table {
	row := make(Row, len(cells))
	copy(row, cells)
	t.rows = append(t.rows, row)
	return t
}

func (t *Table) WithTerminalWidth(w int) *Table {
	t.termWidth = w
	return t
}

func (t *Table) WithIndent(indent string) *Table {
	t.indent = indent
	return t
}

func (t *Table) WithPadding(pad int) *Table {
	t.padding = pad
	return t
}

func (t *Table) WithDivider(show bool) *Table {
	t.showDivider = show
	return t
}

func (t *Table) WithDividerStyle(s lipgloss.Style) *Table {
	t.dividerStyle = s
	return t
}

func (t *Table) WithPagination(page, limit int) *Table {
	if page < 1 {
		page = 1
	}
	t.page = page
	t.limit = limit
	return t
}

