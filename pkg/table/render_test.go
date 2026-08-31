package table

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTableRenderAlignment(t *testing.T) {
	col1 := Column{Title: "REPOSITORY", HeaderStyle: lipgloss.NewStyle().Bold(true)}
	col2 := Column{Title: "PATH", HeaderStyle: lipgloss.NewStyle().Bold(true), Flexible: true}
	col3 := Column{Title: "BRANCH", HeaderStyle: lipgloss.NewStyle().Bold(true)}

	tbl := New(col1, col2, col3).WithTerminalWidth(120)
	tbl.AddRow("ops", "clients/fryto/ops", "main")
	tbl.AddRow("manova-frontend", "clients/manova/manova-frontend", "main")
	tbl.AddRow("ts", "manovaspace/ts", "main")

	out := tbl.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (header, divider, 3 rows), got %d:\n%s", len(lines), out)
	}

	// Verify each column header matches the row columns visually
	widths := tbl.CalculateWidths()
	if widths[0] < 15 { // "manova-frontend" is 15 chars
		t.Errorf("col 0 width should be >= 15, got %d", widths[0])
	}
	if widths[1] < 30 { // "clients/manova/manova-frontend" is 30 chars
		t.Errorf("col 1 width should be >= 30, got %d", widths[1])
	}
}

func TestTableResponsiveTruncation(t *testing.T) {
	col1 := Column{Title: "REPO", Flexible: false}
	col2 := Column{Title: "PATH", Flexible: true, MinWidth: 10}
	col3 := Column{Title: "STATUS", Flexible: false}

	// Force a narrow terminal of 45 cols
	tbl := New(col1, col2, col3).WithTerminalWidth(45)
	tbl.AddRow("manova-frontend", "clients/manova/manova-frontend-super-long-path", "clean")

	out := tbl.String()
	for _, line := range strings.Split(out, "\n") {
		lineWidth := StringWidth(line)
		if lineWidth > 45 {
			t.Errorf("line width %d exceeded terminal width 45: %q", lineWidth, line)
		}
	}
	if !strings.Contains(out, "…") {
		t.Errorf("expected output to contain ellipsis truncation '…', got:\n%s", out)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{name: "fits within maxLen", input: "short", maxLen: 10, expected: "short"},
		{name: "exact length", input: "exact", maxLen: 5, expected: "exact"},
		{name: "truncated with ellipsis", input: "hello world", maxLen: 6, expected: "hello…"},
		{name: "maxLen 1", input: "hello", maxLen: 1, expected: "…"},
		{name: "maxLen 0", input: "hello", maxLen: 0, expected: ""},
		{name: "negative maxLen", input: "hello", maxLen: -1, expected: ""},
		{name: "unicode chars", input: "abcdefghij", maxLen: 5, expected: "abcd…"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateString(tc.input, tc.maxLen)
			if got != tc.expected {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.expected)
			}
			if StringWidth(got) > tc.maxLen && tc.maxLen > 0 {
				t.Errorf("TruncateString result width %d > maxLen %d", StringWidth(got), tc.maxLen)
			}
		})
	}
}

func TestTruncatePath(t *testing.T) {
	path := "clients/manova/manova-frontend"
	res := TruncatePath(path, 15)
	if StringWidth(res) > 15 {
		t.Errorf("TruncatePath result %q width %d > 15", res, StringWidth(res))
	}
	if !strings.HasPrefix(res, "clients/") || !strings.Contains(res, "…") {
		t.Errorf("unexpected truncated path %q", res)
	}

	tests := []struct {
		name   string
		path   string
		maxLen int
	}{
		{name: "path fits", path: "orbit/cli", maxLen: 20},
		{name: "short maxLen", path: "orbit/orbit-cli/pkg/table", maxLen: 4},
		{name: "zero maxLen", path: "orbit/cli", maxLen: 0},
		{name: "single segment", path: "singlepart", maxLen: 6},
		{name: "preserve prefix and suffix", path: "a/b/c/d/e", maxLen: 8},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncatePath(tc.path, tc.maxLen)
			if StringWidth(got) > tc.maxLen && tc.maxLen > 0 {
				t.Errorf("TruncatePath(%q, %d) result %q width %d > %d", tc.path, tc.maxLen, got, StringWidth(got), tc.maxLen)
			}
		})
	}
}

func TestDetectTerminalWidth(t *testing.T) {
	// Test env override
	origEnv := os.Getenv("COLUMNS")
	defer os.Setenv("COLUMNS", origEnv)

	os.Setenv("COLUMNS", "150")
	w := DetectTerminalWidth()
	// Note: if stdout is a TTY in some test runner, term.GetSize may win over COLUMNS, but if not it will be 150
	if w <= 0 {
		t.Errorf("DetectTerminalWidth returned %d, want > 0", w)
	}

	os.Setenv("COLUMNS", "invalid")
	w2 := DetectTerminalWidth()
	if w2 <= 0 {
		t.Errorf("DetectTerminalWidth returned %d, want > 0", w2)
	}
}

func TestTableRenderAlignments(t *testing.T) {
	colLeft := Column{Title: "LEFT", Align: AlignLeft, MinWidth: 10}
	colCenter := Column{Title: "CENTER", Align: AlignCenter, MinWidth: 10}
	colRight := Column{Title: "RIGHT", Align: AlignRight, MinWidth: 10}

	tbl := New(colLeft, colCenter, colRight).WithTerminalWidth(80).WithDivider(false)
	tbl.AddRow("a", "b", "c")

	var buf bytes.Buffer
	err := tbl.Render(&buf)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 { // Header + 1 row
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
}

func TestTableRenderEmpty(t *testing.T) {
	tbl := New()
	if len(tbl.CalculateWidths()) != 0 {
		t.Errorf("expected empty widths for table without columns")
	}
	out := tbl.String()
	if out != "" {
		t.Errorf("expected empty string for table without columns, got %q", out)
	}
}

func TestTableRenderNoFlexibleColumns(t *testing.T) {
	col1 := Column{Title: "COL1", Flexible: false}
	col2 := Column{Title: "COL2", Flexible: false}

	// Narrow width forces truncation even without Flexible=true
	tbl := New(col1, col2).WithTerminalWidth(15)
	tbl.AddRow("very-long-first-column", "very-long-second-column")

	out := tbl.String()
	for _, line := range strings.Split(out, "\n") {
		lineWidth := StringWidth(line)
		if lineWidth > 15 {
			t.Errorf("line width %d exceeded terminal width 15: %q", lineWidth, line)
		}
	}
}

func TestTableRenderMaxWidth(t *testing.T) {
	col1 := Column{Title: "REPO", MaxWidth: 10}
	tbl := New(col1).WithTerminalWidth(120)
	tbl.AddRow("super-long-repository-name")

	widths := tbl.CalculateWidths()
	if widths[0] > 10 {
		t.Errorf("col width %d > MaxWidth 10", widths[0])
	}
}

func TestTableStyleCellPreservationOnTruncation(t *testing.T) {
	col1 := Column{Title: "STATUS", Flexible: true, MinWidth: 5}
	customStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	tbl := New(col1).WithTerminalWidth(10).WithIndent("")
	tbl.AddStyledRow(StyleCell("very-long-status-error-text", customStyle))

	out := tbl.String()
	if !strings.Contains(out, "…") {
		t.Errorf("expected truncated text with ellipsis, got %q", out)
	}
	widths := tbl.CalculateWidths()
	expectedTruncated := customStyle.Render(TruncateString("very-long-status-error-text", widths[0]))
	if !strings.Contains(out, expectedTruncated) {
		t.Errorf("expected output to contain styled truncated text %q, got %q", expectedTruncated, out)
	}
}

func TestTablePagination(t *testing.T) {
	col := Column{Title: "ITEM"}
	tbl := New(col).WithTerminalWidth(80).WithDivider(false)
	for i := 1; i <= 5; i++ {
		tbl.AddRow(strings.Repeat("item-", 1) + string(rune('0'+i)))
	}

	// Page 1, limit 2
	tbl.WithPagination(1, 2)
	out1 := tbl.String()
	if !strings.Contains(out1, "item-1") || !strings.Contains(out1, "item-2") {
		t.Errorf("expected page 1 to have item-1 and item-2, got:\n%s", out1)
	}
	if strings.Contains(out1, "item-3") {
		t.Errorf("page 1 should not have item-3")
	}
	if !strings.Contains(out1, "Showing 1-2 of 5 rows (Page 1/3)") {
		t.Errorf("expected pagination footer, got:\n%s", out1)
	}

	// Page 3, limit 2 (last page with 1 item)
	tbl.WithPagination(3, 2)
	out3 := tbl.String()
	if !strings.Contains(out3, "item-5") {
		t.Errorf("expected page 3 to have item-5, got:\n%s", out3)
	}
	if strings.Contains(out3, "item-4") {
		t.Errorf("page 3 should not have item-4")
	}
	if !strings.Contains(out3, "Showing 5-5 of 5 rows (Page 3/3)") {
		t.Errorf("expected pagination footer for page 3, got:\n%s", out3)
	}

	// Out of bounds page 4, limit 2
	tbl.WithPagination(4, 2)
	out4 := tbl.String()
	if strings.Contains(out4, "item-") {
		t.Errorf("out of bounds page should have no items, got:\n%s", out4)
	}
	if !strings.Contains(out4, "Page 4 of 3") {
		t.Errorf("expected out of bounds footer, got:\n%s", out4)
	}
}

