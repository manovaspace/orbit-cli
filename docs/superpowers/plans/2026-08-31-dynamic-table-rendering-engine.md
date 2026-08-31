# Dynamic Table Rendering Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a reusable, ANSI-aware dynamic table rendering engine with terminal width detection and responsive truncation, and migrate all tabular commands across Orbit CLI (`status`, `invite list`, `migrate status`, `config list`, `staff list`).

**Architecture:** Create `pkg/table` providing visual-width cell measurement, dynamic column allocation, terminal width awareness (`golang.org/x/term`), and flexible column truncation with ellipsis. Refactor CLI commands in `cmd/orbit` to use the unified table engine with styled headers and aligned dividers.

**Tech Stack:** Go 1.26, Lipgloss (`github.com/charmbracelet/lipgloss`), Term (`golang.org/x/term`), Cobra (`github.com/spf13/cobra`).

## Global Constraints

- Must accurately measure visible terminal column width ignoring ANSI escape codes using `lipgloss.Width`.
- Must support terminal width detection with fallback to `$COLUMNS` or 120 columns for non-TTY / piped output.
- Must dynamically truncate flexible columns with ellipsis (`…`) when the rendered table exceeds terminal width.
- Must preserve non-flexible columns (e.g. `BRANCH`, `SYNC`, `WORKING TREE`, `STATUS`) to avoid truncating status indicators.
- Must eliminate all hardcoded byte-counting format strings (`%-22s`, `%-28s`) and double-padding bugs across CLI commands.
- All existing tests in `orbit-cli` must continue to pass (`go test ./...`).

---

### Task 1: Table Engine Core Data Structures & ANSI Measurement (`pkg/table`)

**Files:**
- Create: `pkg/table/table.go`
- Create: `pkg/table/column.go`
- Create: `pkg/table/measure.go`
- Test: `pkg/table/table_test.go`

**Interfaces:**
- Produces:
  ```go
  package table

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

  type Table struct {
      columns      []Column
      rows         []Row
      termWidth    int
      padding      int
      indent       string
      dividerStyle lipgloss.Style
      showDivider  bool
  }

  func New(columns ...Column) *Table
  func (t *Table) AddRow(values ...string) *Table
  func (t *Table) AddStyledRow(cells ...Cell) *Table
  func (t *Table) WithTerminalWidth(w int) *Table
  func (t *Table) WithIndent(indent string) *Table
  func (t *Table) WithPadding(pad int) *Table
  func (t *Table) WithDivider(show bool) *Table
  func (t *Table) WithDividerStyle(s lipgloss.Style) *Table
  func StringWidth(s string) int
  func PlainCell(text string) Cell
  func StyledCell(text string, styled string) Cell
  ```

- [ ] **Step 1: Write failing unit test for core types and ANSI measurement**

Create `pkg/table/table_test.go`:
```go
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

	if len(tbl.columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tbl.columns))
	}
	if len(tbl.rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tbl.rows))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/table`
Expected: FAIL due to missing `pkg/table` package.

- [ ] **Step 3: Implement `pkg/table/column.go`, `pkg/table/measure.go`, and `pkg/table/table.go`**

Create `pkg/table/column.go`:
```go
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
```

Create `pkg/table/measure.go`:
```go
package table

import (
	"github.com/charmbracelet/lipgloss"
)

func StringWidth(s string) int {
	return lipgloss.Width(s)
}
```

Create `pkg/table/table.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/table`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add pkg/table/
git -C orbit/orbit-cli commit -m "feat(table): add core data structures and visual width measurement"
```

---

### Task 2: Dynamic Width Calculation & Responsive Truncation (`pkg/table`)

**Files:**
- Create: `pkg/table/render.go`
- Create: `pkg/table/truncate.go`
- Create: `pkg/table/term.go`
- Test: `pkg/table/render_test.go`

**Interfaces:**
- Produces:
  ```go
  func (t *Table) Render(w io.Writer) error
  func (t *Table) String() string
  func (t *Table) CalculateWidths() []int
  func DetectTerminalWidth() int
  func TruncateString(s string, maxLen int) string
  func TruncatePath(path string, maxLen int) string
  ```

- [ ] **Step 1: Write failing tests for layout, width calculation, and truncation**

Create `pkg/table/render_test.go`:
```go
package table

import (
	"bytes"
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

func TestTruncatePath(t *testing.T) {
	path := "clients/manova/manova-frontend"
	res := TruncatePath(path, 15)
	if StringWidth(res) > 15 {
		t.Errorf("TruncatePath result %q width %d > 15", res, StringWidth(res))
	}
	if !strings.HasPrefix(res, "clients/") || !strings.Contains(res, "…") {
		t.Errorf("unexpected truncated path %q", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./pkg/table`
Expected: FAIL due to missing `CalculateWidths`, `Render`, `TruncatePath`.

- [ ] **Step 3: Implement `pkg/table/term.go`, `pkg/table/truncate.go`, and `pkg/table/render.go`**

Create `pkg/table/term.go`:
```go
package table

import (
	"os"
	"strconv"

	"golang.org/x/term"
)

const DefaultFallbackWidth = 120

func DetectTerminalWidth() int {
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			return w
		}
	}

	if cols := os.Getenv("COLUMNS"); cols != "" {
		if w, err := strconv.Atoi(cols); err == nil && w > 0 {
			return w
		}
	}

	return DefaultFallbackWidth
}
```

Create `pkg/table/truncate.go`:
```go
package table

import (
	"strings"
)

func TruncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if StringWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}

	runes := []rune(s)
	for i := len(runes) - 1; i >= 0; i-- {
		candidate := string(runes[:i]) + "…"
		if StringWidth(candidate) <= maxLen {
			return candidate
		}
	}
	return "…"
}

func TruncatePath(path string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if StringWidth(path) <= maxLen {
		return path
	}
	if maxLen <= 5 {
		return TruncateString(path, maxLen)
	}

	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return TruncateString(path, maxLen)
	}

	prefix := parts[0] + "/"
	suffix := parts[len(parts)-1]

	candidate := prefix + "…/" + suffix
	if StringWidth(candidate) <= maxLen {
		return candidate
	}

	candidate = "…/" + suffix
	if StringWidth(candidate) <= maxLen {
		return candidate
	}

	return TruncateString(path, maxLen)
}
```

Create `pkg/table/render.go`:
```go
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
				if row[i].Style != nil {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/table`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add pkg/table/
git -C orbit/orbit-cli commit -m "feat(table): add dynamic width calculation, terminal detection, and responsive truncation"
```

---

### Task 3: Migrate `orbit status` (`cmd/orbit/status.go`)

**Files:**
- Modify: `cmd/orbit/status.go`
- Test: `cmd/orbit/status_test.go`

**Interfaces:**
- Consumes: `pkg/table`
- Produces: Dynamic responsive status table rendering in `cmd/orbit/status.go`

- [ ] **Step 1: Write integration test verifying status table rendering and alignment**

Create/update `cmd/orbit/status_test.go`:
```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestStatusTableOutputStructure(t *testing.T) {
	// Verify that table output contains aligned columns and headers
	out := &bytes.Buffer{}
	cmd := newStatusCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all"})

	// Run status against the current repo/workspace
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error running status: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "REPOSITORY") || !strings.Contains(output, "PATH") || !strings.Contains(output, "BRANCH") {
		t.Errorf("status output missing expected headers:\n%s", output)
	}
	if !strings.Contains(output, "WORKING TREE") {
		t.Errorf("status output missing WORKING TREE header:\n%s", output)
	}
}
```

- [ ] **Step 2: Run test to observe current behavior**

Run: `go test -v ./cmd/orbit -run TestStatusTableOutputStructure`

- [ ] **Step 3: Refactor `cmd/orbit/status.go` to use `pkg/table`**

Update `cmd/orbit/status.go` table rendering block:
```go
// Replace lines 49-140 in cmd/orbit/status.go with:
tbl := table.New(
    table.Column{Title: "REPOSITORY", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 16},
    table.Column{Title: "PATH", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 20, Flexible: true},
    table.Column{Title: "BRANCH", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 8},
    table.Column{Title: "SYNC", HeaderStyle: headerStyle, MinWidth: 12},
    table.Column{Title: "WORKING TREE", HeaderStyle: headerStyle, MinWidth: 14},
)

for _, s := range statuses {
    repoCell := table.PlainCell(s.Name)
    pathCell := table.PlainCell(s.Path)

    switch s.Error {
    case "":
        branchCell := table.PlainCell(s.Branch)
        var syncCell table.Cell
        switch {
        case s.Ahead > 0 && s.Behind > 0:
            syncCell = table.StyledCell(fmt.Sprintf("↕%d/%d diverged", s.Ahead, s.Behind), warningStyle.Render(fmt.Sprintf("↕%d/%d diverged", s.Ahead, s.Behind)))
        case s.Ahead > 0:
            syncCell = table.StyledCell(fmt.Sprintf("↑%d ahead", s.Ahead), subtleStyle.Render(fmt.Sprintf("↑%d ahead", s.Ahead)))
        case s.Behind > 0:
            syncCell = table.StyledCell(fmt.Sprintf("↓%d behind", s.Behind), subtleStyle.Render(fmt.Sprintf("↓%d behind", s.Behind)))
        default:
            syncCell = table.StyledCell("up to date", subtleStyle.Render("up to date"))
        }

        var treeCell table.Cell
        if s.Clean {
            cleanCount++
            treeCell = table.StyledCell("✔ clean", successStyle.Render("✔ clean"))
        } else {
            dirtyCount++
            treeCell = table.StyledCell(fmt.Sprintf("✖ dirty (%d)", s.DirtyFiles), errorStyle.Render(fmt.Sprintf("✖ dirty (%d)", s.DirtyFiles)))
        }

        tbl.AddStyledRow(repoCell, pathCell, branchCell, syncCell, treeCell)
    case orchestrator.ErrMissing:
        missingCount++
        tbl.AddStyledRow(repoCell, pathCell, table.StyledCell("-", subtleStyle.Render("-")), table.StyledCell("-", subtleStyle.Render("-")), table.StyledCell("not cloned", subtleStyle.Render("not cloned")))
    case orchestrator.ErrGitless:
        gitlessCount++
        tbl.AddStyledRow(repoCell, pathCell, table.StyledCell("-", subtleStyle.Render("-")), table.StyledCell("-", subtleStyle.Render("-")), table.StyledCell("gitless — run orbit repair", warningStyle.Render("gitless — run orbit repair")))
    default:
        otherErrCount++
        tbl.AddStyledRow(repoCell, pathCell, table.StyledCell("-", subtleStyle.Render("-")), table.StyledCell("-", subtleStyle.Render("-")), table.StyledCell(s.Error, errorStyle.Render(s.Error)))
    }
}

fmt.Fprintln(out)
_ = tbl.Render(out)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./cmd/orbit -run TestStatusTableOutputStructure`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/status.go cmd/orbit/status_test.go
git -C orbit/orbit-cli commit -m "refactor(status): migrate workspace status table to dynamic table engine"
```

---

### Task 4: Migrate `orbit invite list` (`cmd/orbit/invite.go`)

**Files:**
- Modify: `cmd/orbit/invite.go`
- Test: `cmd/orbit/invite_test.go`

**Interfaces:**
- Consumes: `pkg/table`
- Produces: Dynamic responsive table rendering for `orbit invite list`

- [ ] **Step 1: Write test for invite list table output**

Verify existing tests in `cmd/orbit/invite_test.go` and add test for table layout rendering.

- [ ] **Step 2: Refactor `cmd/orbit/invite.go` `newInviteListCmd` to use `pkg/table`**

Update `newInviteListCmd` in `cmd/orbit/invite.go`:
```go
tbl := table.New(
    table.Column{Title: "INVITE ID", HeaderStyle: headerStyle, CellStyle: codeStyle, MinWidth: 12},
    table.Column{Title: "EMAIL", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 18, Flexible: true},
    table.Column{Title: "NAME", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 10, Flexible: true},
    table.Column{Title: "SCOPE", HeaderStyle: headerStyle, CellStyle: infoStyle, MinWidth: 8},
    table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 10},
    table.Column{Title: "EXPIRES", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 18},
    table.Column{Title: "CREATED", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 18},
)

for _, r := range records {
    status := r.Status()
    var statusCell table.Cell
    switch status {
    case "active":
        activeCount++
        statusCell = table.StyledCell("✔ active", successStyle.Render("✔ active"))
    case "revoked":
        revokedCount++
        statusCell = table.StyledCell("✖ revoked", errorStyle.Render("✖ revoked"))
    case "expired":
        expiredCount++
        statusCell = table.StyledCell("⚠ expired", warningStyle.Render("⚠ expired"))
    default:
        statusCell = table.StyledCell(status, subtleStyle.Render(status))
    }

    nameVal := r.DisplayName
    if nameVal == "" {
        nameVal = "-"
    }

    idVal := r.ID
    if len(idVal) > 16 {
        idVal = idVal[:16] + "…"
    }

    tbl.AddStyledRow(
        table.PlainCell(idVal),
        table.PlainCell(r.Email),
        table.PlainCell(nameVal),
        table.PlainCell(r.Scope),
        statusCell,
        table.PlainCell(r.ExpiresAt.Format("2006-01-02 15:04 UTC")),
        table.PlainCell(r.IssuedAt.Format("2006-01-02 15:04 UTC")),
    )
}

fmt.Fprintln(out)
_ = tbl.Render(out)
```

- [ ] **Step 3: Run invite tests**

Run: `go test -v ./cmd/orbit -run TestInvite`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/invite.go cmd/orbit/invite_test.go
git -C orbit/orbit-cli commit -m "refactor(invite): migrate invite list to dynamic table engine"
```

---

### Task 5: Migrate `orbit migrate status` (`cmd/orbit/migrate.go`)

**Files:**
- Modify: `cmd/orbit/migrate.go`
- Test: `cmd/orbit/migrate_test.go` (or `pkg/migrate/migrate_test.go`)

**Interfaces:**
- Consumes: `pkg/table`
- Produces: Dynamic responsive table rendering for `orbit migrate status`

- [ ] **Step 1: Refactor `cmd/orbit/migrate.go` to use `pkg/table`**

Update `newMigrateStatusCmd` in `cmd/orbit/migrate.go`:
```go
tbl := table.New(
    table.Column{Title: "MIGRATION ID", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 24},
    table.Column{Title: "DESCRIPTION", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 30, Flexible: true},
    table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 16},
)

for _, m := range allMigrations {
    idCell := table.PlainCell(m.ID)
    descCell := table.PlainCell(m.Description)

    var statusCell table.Cell
    if appliedAt, ok := appliedMap[m.ID]; ok {
        appliedCount++
        statusStr := fmt.Sprintf("✔ Applied (%s)", appliedAt.Format("2006-01-02 15:04"))
        statusCell = table.StyledCell(statusStr, successStyle.Render(statusStr))
    } else {
        pendingCount++
        statusCell = table.StyledCell("⚠ Pending", warningStyle.Render("⚠ Pending"))
    }

    tbl.AddStyledRow(idCell, descCell, statusCell)
}

fmt.Fprintln(out)
_ = tbl.Render(out)
```

- [ ] **Step 2: Run migrate tests**

Run: `go test -v ./pkg/migrate/... ./cmd/orbit/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/migrate.go
git -C orbit/orbit-cli commit -m "refactor(migrate): migrate migration status table to dynamic table engine"
```

---

### Task 6: Migrate `orbit config list` (`cmd/orbit/config.go`)

**Files:**
- Modify: `cmd/orbit/config.go`
- Test: `cmd/orbit/config_test.go`

**Interfaces:**
- Consumes: `pkg/table`
- Produces: Dynamic responsive table rendering for `orbit config list`

- [ ] **Step 1: Refactor `cmd/orbit/config.go` to use `pkg/table`**

Update `newConfigListCmd` in `cmd/orbit/config.go`:
```go
tbl := table.New(
    table.Column{Title: "KEY", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 18},
    table.Column{Title: "VALUE", HeaderStyle: headerStyle, CellStyle: codeStyle, MinWidth: 20, Flexible: true},
    table.Column{Title: "TYPE", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 8},
    table.Column{Title: "SOURCE", HeaderStyle: headerStyle, MinWidth: 14, Flexible: true},
)

for _, e := range entries {
    kCell := table.PlainCell(e.Key)
    vCell := table.PlainCell(e.Value)
    tCell := table.PlainCell(e.Type)
    sCell := table.StyledCell(formatSourcePlainText(e), formatSource(e))

    tbl.AddStyledRow(kCell, vCell, tCell, sCell)
}

fmt.Fprintln(out)
_ = tbl.Render(out)
```

- [ ] **Step 2: Run config tests**

Run: `go test -v ./cmd/orbit -run TestConfig`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/config.go cmd/orbit/config_test.go
git -C orbit/orbit-cli commit -m "refactor(config): migrate config list table to dynamic table engine"
```

---

### Task 7: Migrate `orbit staff list` (`cmd/orbit/staff.go`)

**Files:**
- Modify: `cmd/orbit/staff.go`
- Test: `cmd/orbit/staff_test.go`

**Interfaces:**
- Consumes: `pkg/table`
- Produces: Dynamic responsive table rendering for `orbit staff list`

- [ ] **Step 1: Refactor `cmd/orbit/staff.go` `newStaffListCmd` to use `pkg/table`**

Update `newStaffListCmd` in `cmd/orbit/staff.go`:
```go
tbl := table.New(
    table.Column{Title: "UID", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 12},
    table.Column{Title: "NAME", HeaderStyle: headerStyle, MinWidth: 16, Flexible: true},
    table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 10},
    table.Column{Title: "FORWARD EMAIL", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 20, Flexible: true},
)

for _, s := range list {
    var statusCell table.Cell
    switch strings.ToLower(s.Status) {
    case "active", "enabled":
        statusCell = table.StyledCell("✔ active", successStyle.Render("✔ active"))
    case "disabled":
        statusCell = table.StyledCell("✖ disabled", errorStyle.Render("✖ disabled"))
    default:
        statusCell = table.PlainCell(s.Status)
    }

    nameVal := s.DisplayName
    if nameVal == "" {
        nameVal = "-"
    }

    tbl.AddStyledRow(
        table.PlainCell(s.UID),
        table.PlainCell(nameVal),
        statusCell,
        table.PlainCell(s.PersonalForward),
    )
}

_ = tbl.Render(out)
```

- [ ] **Step 2: Run staff tests**

Run: `go test -v ./cmd/orbit -run TestStaff`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/staff.go cmd/orbit/staff_test.go
git -C orbit/orbit-cli commit -m "refactor(staff): migrate staff list to dynamic table engine"
```

---

### Task 8: End-to-End Test Suite Verification & Live Rendering Validation

**Files:**
- All packages in `orbit/orbit-cli`

- [ ] **Step 1: Run full test suite**

Run: `go test ./...` in `orbit/orbit-cli`
Expected: All packages pass.

- [ ] **Step 2: Verify binary compilation**

Run: `go build -o bin/orbit ./cmd/orbit`
Expected: Compiles cleanly with exit code 0.

- [ ] **Step 3: Run live CLI status test**

Run: `./bin/orbit status all`
Expected: Pristine column alignment across all repositories regardless of name/path length.

- [ ] **Step 4: Final commit & cleanup if needed**

```bash
git -C orbit/orbit-cli status
```
