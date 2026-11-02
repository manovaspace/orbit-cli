package render

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/manovaspace/orbit-cli/pkg/table"
)

// sectionWidth is the fixed width of a section rule in runes.
const sectionWidth = 60

// trimTrailingSpace removes spaces and tabs before a line ending while keeping
// any ANSI reset sequence that followed the padding.
var trimTrailingSpace = regexp.MustCompile(`[ \t]+((?:\x1b\[[0-9;]*[a-zA-Z])*)\n`)

// SummaryPart is one counter in a summary line.
type SummaryPart struct {
	Status Status
	Count  int
	Label  string
}

// Option configures a Printer.
type Option func(*Printer)

// WithPlain makes the printer emit plain text: status icons become bracketed
// labels such as [ok] and no lipgloss style is applied. Use it for tests and
// for callers that cannot render ANSI.
func WithPlain() Option {
	return func(p *Printer) { p.plain = true }
}

// Printer writes the text form of a command result. It remembers the first
// write error so a caller can render a whole result and check Err once.
type Printer struct {
	w     io.Writer
	plain bool
	err   error
	wrote bool
}

// NewPrinter returns a Printer writing to w.
func NewPrinter(w io.Writer, opts ...Option) *Printer {
	p := &Printer{w: w}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

// Err returns the first write error, if any.
func (p *Printer) Err() error {
	return p.err
}

// Title writes a bold title line followed by one truly empty line.
func (p *Printer) Title(text string) {
	p.write(p.styled(Title, text) + "\n\n")
}

// Section writes a blank line when output already exists, then a fixed-width
// "── name ─────" rule.
func (p *Printer) Section(name string) {
	if p.wrote {
		p.write("\n")
	}
	p.write(p.styled(Header, sectionLine(name)) + "\n")
}

// KV writes one label/value pair aligned to its own label.
func (p *Printer) KV(label, value string) {
	p.KVs([2]string{label, value})
}

// KVs writes label/value pairs indented by two spaces, with labels aligned to
// the longest label in the call.
func (p *Printer) KVs(pairs ...[2]string) {
	if len(pairs) == 0 {
		return
	}
	width := 0
	for _, pair := range pairs {
		if w := lipgloss.Width(pair[0]); w > width {
			width = w
		}
	}
	for _, pair := range pairs {
		line := "  " + p.styled(Subtle, pair[0])
		if pair[1] != "" {
			line += strings.Repeat(" ", width-lipgloss.Width(pair[0])) + "  " + pair[1]
		}
		p.write(line + "\n")
	}
}

// Line writes one status line as "  <icon>  text".
func (p *Printer) Line(s Status, text string) {
	line := "  " + p.statusIcon(s)
	if text != "" {
		line += "  " + text
	}
	p.write(line + "\n")
}

// Table renders t with the shared table package. The table pads cells to
// column width, so trailing padding is trimmed before it reaches the writer;
// ANSI reset sequences are kept.
func (p *Printer) Table(t *table.Table) error {
	if t == nil {
		return nil
	}
	if p.err != nil {
		return p.err
	}
	var buf bytes.Buffer
	if err := t.Render(&buf); err != nil {
		p.err = err
		return err
	}
	p.write(trimTrailingSpace.ReplaceAllString(buf.String(), "$1\n"))
	return nil
}

// Empty writes the single empty-result wording every command shares.
func (p *Printer) Empty(what string) {
	p.write(p.styled(Subtle, "No "+what+" found.") + "\n")
}

// Summary writes one summary line after a blank line. Zero-count parts are
// kept so output stays stable.
func (p *Printer) Summary(parts ...SummaryPart) {
	if len(parts) == 0 {
		return
	}
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		label := part.Label
		if label == "" {
			label = part.Status.Label()
		}
		rendered = append(rendered, fmt.Sprintf("%s %d %s", p.statusIcon(part.Status), part.Count, label))
	}
	p.write("\n" + strings.Join(rendered, "  ") + "\n")
}

// SummaryText writes a free-form summary line after a blank line.
func (p *Printer) SummaryText(text string) {
	p.write("\n" + text + "\n")
}

// Hint writes an indented hint line, for use after an error.
func (p *Printer) Hint(text string) {
	p.write(p.styled(Subtle, "  hint: "+text) + "\n")
}

// Blank writes one empty line.
func (p *Printer) Blank() {
	p.write("\n")
}

func (p *Printer) write(s string) {
	if p.err != nil {
		return
	}
	if _, err := io.WriteString(p.w, s); err != nil {
		p.err = err
		return
	}
	p.wrote = true
}

func (p *Printer) styled(style lipgloss.Style, text string) string {
	if p.plain {
		return text
	}
	return style.Render(text)
}

func (p *Printer) statusIcon(s Status) string {
	if p.plain {
		return "[" + s.Label() + "]"
	}
	return s.Icon()
}

// sectionLine builds "── name ─────" at exactly sectionWidth runes, truncating
// or padding the name by rune width.
func sectionLine(name string) string {
	const prefix = "── "
	line := prefix
	if name != "" {
		name = truncateWidth(name, sectionWidth-lipgloss.Width(prefix)-1)
		line += name + " "
	}
	if pad := sectionWidth - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat("─", pad)
	}
	return line
}

// truncateWidth cuts s so its rendered width is at most width runes.
func truncateWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String()
}
