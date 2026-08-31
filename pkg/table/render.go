package table

import (
	"fmt"
	"io"
	"strings"
)

func (t *Table) CalculateWidths() []int {
	numCols := len(t.columns)
	if numCols == 0 {
		return nil
	}

	naturalWidths := make([]int, numCols)
	for i, col := range t.columns {
		w := StringWidth(col.Title)
		if col.MinWidth > w {
			w = col.MinWidth
		}
		naturalWidths[i] = w
	}

	for _, row := range t.rows {
		for i := 0; i < numCols && i < len(row); i++ {
			w := StringWidth(row[i].Text)
			if w > naturalWidths[i] {
				naturalWidths[i] = w
			}
		}
	}

	for i, col := range t.columns {
		if col.MaxWidth > 0 && naturalWidths[i] > col.MaxWidth {
			naturalWidths[i] = col.MaxWidth
		}
	}

	termW := t.termWidth
	if termW <= 0 {
		termW = DetectTerminalWidth()
	}

	indentW := StringWidth(t.indent)
	totalPad := (numCols - 1) * t.padding
	available := termW - indentW - totalPad
	if available < numCols*4 {
		available = numCols * 4
	}

	totalNatural := 0
	for _, w := range naturalWidths {
		totalNatural += w
	}

	if totalNatural <= available {
		return naturalWidths
	}

	// Truncation pass: distribute reduction across flexible columns
	widths := make([]int, numCols)
	copy(widths, naturalWidths)
	excess := totalNatural - available

	var flexibleIndices []int
	for i, col := range t.columns {
		if col.Flexible {
			flexibleIndices = append(flexibleIndices, i)
		}
	}

	if len(flexibleIndices) == 0 {
		// If no flexible columns declared, treat all columns as flexible
		for i := range t.columns {
			flexibleIndices = append(flexibleIndices, i)
		}
	}

	// Shrink flexible columns down towards MinWidth
	for excess > 0 {
		shrunkAny := false
		for _, idx := range flexibleIndices {
			minW := t.columns[idx].MinWidth
			if minW < 4 {
				minW = 4
			}
			if widths[idx] > minW && excess > 0 {
				widths[idx]--
				excess--
				shrunkAny = true
			}
		}
		if !shrunkAny {
			break
		}
	}

	// If still excess, shrink flexible columns further down to 3
	for excess > 0 {
		shrunkAny := false
		for _, idx := range flexibleIndices {
			if widths[idx] > 3 && excess > 0 {
				widths[idx]--
				excess--
				shrunkAny = true
			}
		}
		if !shrunkAny {
			break
		}
	}

	return widths
}

func (t *Table) Render(w io.Writer) error {
	numCols := len(t.columns)
	if numCols == 0 {
		return nil
	}

	widths := t.CalculateWidths()
	padStr := strings.Repeat(" ", t.padding)

	// Render Header
	var headerParts []string
	for i, col := range t.columns {
		title := col.Title
		cellW := widths[i]
		if StringWidth(title) > cellW {
			title = TruncateString(title, cellW)
		}
		renderedTitle := col.HeaderStyle.Render(title)
		padded := padCell(title, renderedTitle, cellW, col.Align)
		headerParts = append(headerParts, padded)
	}
	fmt.Fprintf(w, "%s%s\n", t.indent, strings.Join(headerParts, padStr))

	// Render Divider Line
	if t.showDivider {
		totalW := 0
		for _, w := range widths {
			totalW += w
		}
		totalW += (numCols - 1) * t.padding
		divider := t.dividerStyle.Render(strings.Repeat("─", totalW))
		fmt.Fprintf(w, "%s%s\n", t.indent, divider)
	}

	// Render Rows
	for _, row := range t.rows {
		var rowParts []string
		for i, col := range t.columns {
			cellText := ""
			cellStyled := ""
			if i < len(row) {
				cellText = row[i].Text
				cellStyled = row[i].Styled
				if cellStyled == "" {
					if row[i].Style != nil {
						cellStyled = row[i].Style.Render(cellText)
					} else {
						cellStyled = col.CellStyle.Render(cellText)
					}
				}
			}

			cellW := widths[i]
			if StringWidth(cellText) > cellW {
				if col.Flexible && strings.Contains(cellText, "/") {
					cellText = TruncatePath(cellText, cellW)
				} else {
					cellText = TruncateString(cellText, cellW)
				}
				if i < len(row) && row[i].Style != nil {
					cellStyled = row[i].Style.Render(cellText)
				} else {
					cellStyled = col.CellStyle.Render(cellText)
				}
			}

			padded := padCell(cellText, cellStyled, cellW, col.Align)
			rowParts = append(rowParts, padded)
		}
		fmt.Fprintf(w, "%s%s\n", t.indent, strings.Join(rowParts, padStr))
	}

	return nil
}

func (t *Table) String() string {
	var buf strings.Builder
	_ = t.Render(&buf)
	return buf.String()
}

func padCell(plain string, styled string, targetWidth int, align Alignment) string {
	visualWidth := StringWidth(plain)
	if visualWidth >= targetWidth {
		return styled
	}

	diff := targetWidth - visualWidth
	switch align {
	case AlignRight:
		return strings.Repeat(" ", diff) + styled
	case AlignCenter:
		left := diff / 2
		right := diff - left
		return strings.Repeat(" ", left) + styled + strings.Repeat(" ", right)
	default: // AlignLeft
		return styled + strings.Repeat(" ", diff)
	}
}
