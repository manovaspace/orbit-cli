package table

import (
	"charm.land/lipgloss/v2"
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
	detailColumn int
	detailStyle  lipgloss.Style
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
	row := Row{Cells: make([]Cell, len(values))}
	for i, v := range values {
		row.Cells[i] = PlainCell(v)
	}
	t.rows = append(t.rows, row)
	return t
}

// AddDetailRow adds a row whose detail prints on the line below it. Rows that
// pass no detail render exactly like AddRow.
func (t *Table) AddDetailRow(detail string, values ...string) *Table {
	row := Row{Cells: make([]Cell, len(values)), Detail: detail}
	for i, v := range values {
		row.Cells[i] = PlainCell(v)
	}
	t.rows = append(t.rows, row)
	return t
}

func (t *Table) AddStyledRow(cells ...Cell) *Table {
	row := Row{Cells: make([]Cell, len(cells))}
	copy(row.Cells, cells)
	t.rows = append(t.rows, row)
	return t
}

// WithDetailColumn aligns each row's detail line with the given zero-based
// column. The default, 0, aligns it with the first column.
func (t *Table) WithDetailColumn(column int) *Table {
	if column < 0 {
		column = 0
	}
	t.detailColumn = column
	return t
}

// WithDetailStyle styles each row's detail line. The default is unstyled text.
func (t *Table) WithDetailStyle(style lipgloss.Style) *Table {
	t.detailStyle = style
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
