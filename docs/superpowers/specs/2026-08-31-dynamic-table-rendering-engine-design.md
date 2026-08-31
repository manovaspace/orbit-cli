# Dynamic Table Rendering Engine Design Spec

- **Author**: Orbit Core Engineering & Antigravity
- **Date**: 2026-08-31
- **Status**: Draft / Under Review
- **Scope**: `orbit/orbit-cli` (`pkg/table`, `cmd/orbit/*`)

---

## 1. Problem Statement & Motivation

### Current Issues
1. **ANSI Padding Misalignment:**
   Commands such as `orbit status`, `orbit invite list`, `orbit migrate status`, and `orbit config list` use standard `fmt.Fprintf` format strings (e.g. `%-22s %-28s %-16s`) or `padRight` combined with Lipgloss styled strings. Because ANSI escape sequences (e.g., `\x1b[1;36m`) contain non-printable control bytes, standard byte-counting formatters count ANSI codes toward column widths, causing header labels, divider rules, and data rows to misalign visually in the terminal.
2. **Fixed Column Overflow:**
   Hardcoded widths (e.g. 28 characters for `PATH` in `orbit status`) are routinely exceeded by nested client paths such as `clients/manova/manova-frontend` (30 characters) or `clients/kohan_kherad/website` (28 characters). This pushes subsequent columns to the right, misaligning rows across the table.
3. **No Terminal Width Awareness or Truncation:**
   Wide tables overflow narrow terminal windows (e.g., 80-column panes or mobile split views), resulting in broken line wraps and unreadable logs.
4. **Ad-hoc Output Discrepancies:**
   Commands like `orbit staff list` output raw tab-separated text without headers, borders, or styling consistency.

---

## 2. Architectural Design: `pkg/table`

We introduce a dedicated, lightweight, zero-dependency package at `pkg/table` within `orbit-cli` that encapsulates:
1. ANSI-aware cell measurement (using `lipgloss.Width` / rune visual width).
2. Dynamic column width computation based on natural cell content across all rows.
3. Terminal width detection via `golang.org/x/term.GetSize(os.Stdout.Fd())` with fallback to `$COLUMNS` and environment checks.
4. Responsive truncation for flexible columns when total table width exceeds terminal dimensions.
5. Consistent styling for titles, headers, dividers, and cell alignments.

### 2.1 Core Types & Data Structures

```go
package table

import (
	"io"

	"github.com/charmbracelet/lipgloss"
)

// Alignment defines the text alignment within a table cell.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
	AlignCenter
)

// Column defines metadata, constraints, and styling for a table column.
type Column struct {
	Title       string         // Column header text
	HeaderStyle lipgloss.Style // Style applied to the header title
	CellStyle   lipgloss.Style // Default style applied to data cells in this column
	MinWidth    int            // Minimum printable column width (e.g. 10)
	MaxWidth    int            // Maximum allowed width (0 for unbounded)
	Flexible    bool           // If true, shrinks/truncates when terminal width is constrained
	Align       Alignment      // Cell alignment
}

// Cell represents an individual data cell, supporting raw text and pre-styled ANSI strings.
type Cell struct {
	Text   string         // Plain text content (used for measuring and truncation)
	Styled string         // Optional ANSI pre-styled content (if empty, Text + Column.CellStyle is used)
	Style  *lipgloss.Style // Optional per-cell style override
}

// Row represents a row of table cells.
type Row []Cell

// Table manages the layout, calculation, and rendering of tabular data.
type Table struct {
	columns      []Column
	rows         []Row
	termWidth    int            // Configured or auto-detected terminal width (0 = auto)
	padding      int            // Space between adjacent columns (default: 2)
	indent       string         // Leftmost indentation (default: "  ")
	dividerStyle lipgloss.Style // Style for horizontal rule below headers
	showDivider  bool           // Whether to print header divider line
}
```

### 2.2 Public API

- `table.New(columns ...Column) *Table`: Initialize a new table builder.
- `t.AddRow(values ...string) *Table`: Append a row from plain text strings.
- `t.AddStyledRow(cells ...Cell) *Table`: Append a row with explicit cell formatting.
- `t.WithTerminalWidth(w int) *Table`: Explicitly set terminal width (useful for testing or forced constraints).
- `t.WithIndent(indent string) *Table`: Customize left indentation.
- `t.WithPadding(pad int) *Table`: Customize inter-column spacing.
- `t.Render(w io.Writer) error`: Compute layout and stream rendered table to writer.
- `t.String() string`: Render table as a string.

---

## 3. Visual Width Calculation & Truncation Mechanics

### 3.1 Visual Width Measurement
- Visual width is computed using `lipgloss.Width(str)`, which accurately strips ANSI escape sequences and measures Unicode character widths (including double-width glyphs and emojis like `✔`, `⚠`, `✖`, `↑1`).
- For any column $i$, its natural width $W_i$ is:
  $$W_i = \max\left(\text{VisualWidth}(\text{Title}_i), \max_{r} \text{VisualWidth}(\text{Cell}_{r,i}.\text{Text})\right)$$
- If `MaxWidth > 0`, $W_i = \min(W_i, \text{MaxWidth})$.
- If `MinWidth > 0`, $W_i = \max(W_i, \text{MinWidth})$.

### 3.2 Terminal Width Detection
- Detection queries `term.GetSize(int(os.Stdout.Fd()))`.
- If stdout is not a TTY or detection fails:
  - Check `os.Getenv("COLUMNS")`.
  - Fall back to standard `120` columns (unconstrained but readable baseline).

### 3.3 Dynamic Truncation Algorithm
Let $T$ be the effective available table width:
$$T = \text{TerminalWidth} - \text{VisualWidth}(\text{Indent})$$
Let total required natural width be:
$$N = \sum_{i=1}^k W_i + (k - 1) \times \text{Padding}$$

1. **If $N \le T$:**
   - No truncation needed. Every column renders at its natural width $W_i$.
2. **If $N > T$ (Overflow):**
   - Calculate overflow amount: $\Delta = N - T$.
   - Identify all columns with `Flexible: true`.
   - Distribute the reduction $\Delta$ proportionally or greedily across flexible columns down to their `MinWidth`.
   - If reduction reaches `MinWidth` across all flexible columns and $\Delta > 0$, shrink flexible columns further down to minimum ellipsis size (3 characters: `…`).
   - Non-flexible columns (e.g. `BRANCH`, `SYNC`, `WORKING TREE`, `STATUS`) retain full natural width to ensure status indicators and badges remain intact.

### 3.4 Text Truncation Formatting
- For general text (descriptions, names, values), truncate from right: `strings.TrimSpace(text[:width-1]) + "…"`.
- For file paths (`clients/manova/manova-frontend`), middle-truncation or path-aware truncation is applied when width $< \text{length}$: `clients/…/frontend` or `clients/manova/…`.

---

## 4. Command Migrations

### 4.1 `orbit status [scope]` (`cmd/orbit/status.go`)
- **Columns:**
  - `REPOSITORY` (`Flexible: false`, `boldStyle`)
  - `PATH` (`Flexible: true`, `subtleStyle`, `MinWidth: 16`)
  - `BRANCH` (`Flexible: false`)
  - `SYNC` (`Flexible: false`)
  - `WORKING TREE` (`Flexible: false`)
- Eliminates hardcoded 22/28 byte format strings. Dynamically scales with the longest path in the workspace.

### 4.2 `orbit invite list` (`cmd/orbit/invite.go`)
- **Columns:**
  - `INVITE ID` (`Flexible: false`, `codeStyle`)
  - `EMAIL` (`Flexible: true`, `boldStyle`, `MinWidth: 18`)
  - `NAME` (`Flexible: true`, `subtleStyle`, `MinWidth: 10`)
  - `SCOPE` (`Flexible: false`, `infoStyle`)
  - `STATUS` (`Flexible: false`)
  - `EXPIRES` (`Flexible: false`, `subtleStyle`)
  - `CREATED` (`Flexible: false`, `subtleStyle`)

### 4.3 `orbit migrate status` (`cmd/orbit/migrate.go`)
- **Columns:**
  - `MIGRATION ID` (`Flexible: false`, `boldStyle`)
  - `DESCRIPTION` (`Flexible: true`, `subtleStyle`, `MinWidth: 24`)
  - `STATUS` (`Flexible: false`)

### 4.4 `orbit config list` (`cmd/orbit/config.go`)
- **Columns:**
  - `KEY` (`Flexible: false`, `boldStyle`)
  - `VALUE` (`Flexible: true`, `codeStyle`, `MinWidth: 16`)
  - `TYPE` (`Flexible: false`, `subtleStyle`)
  - `SOURCE` (`Flexible: true`, `MinWidth: 12`)

### 4.5 `orbit staff list` (`cmd/orbit/staff.go`)
- **Columns:**
  - `UID` (`Flexible: false`, `boldStyle`)
  - `DISPLAY NAME` (`Flexible: true`, `MinWidth: 16`)
  - `STATUS` (`Flexible: false`)
  - `FORWARD EMAIL` (`Flexible: true`, `subtleStyle`, `MinWidth: 20`)

---

## 5. Visual Appearance & Styling Consistency

- **Headers:** Styled in bold cyan (`headerStyle`, Lipgloss color `14`).
- **Divider:** Subtle gray line (`subtleStyle`, Lipgloss color `8`) whose width precisely matches the calculated total width of the table.
- **Indent:** Left margin of 2 spaces (`"  "`), consistent with Orbit CLI design guidelines.
- **Row Padding:** 2 spaces between adjacent columns.
- **Piped / Non-TTY Output:** When piped to tools like `grep`, `cat`, or file redirection, clean output without unnecessary truncation is preserved.

---

## 6. Verification & Test Plan

1. **`pkg/table` Unit Tests (`pkg/table/table_test.go`):**
   - Measure pure ASCII vs ANSI colored strings vs Unicode emojis (`✔`, `✖`, `↑1`).
   - Validate column width assignment under various terminal sizes (60, 80, 100, 140, 200 columns).
   - Test flexible column shrinking and truncation with ellipsis (`…`).
   - Test empty rows, single row, multiple rows with varying string lengths.
   - Verify alignment (Left, Right, Center).
2. **Command Integration Tests:**
   - Execute `go test ./...` in `orbit-cli` to ensure all existing CLI and unit tests pass.
   - Test `orbit status` output against sample repository trees with long and short paths.
   - Test `orbit invite list`, `orbit migrate status`, `orbit config list`, and `orbit staff list`.
3. **Manual CLI Verification:**
   - Run `o status` in terminal and verify that `REPOSITORY`, `PATH`, `BRANCH`, `SYNC`, and `WORKING TREE` are completely aligned across header and rows.
