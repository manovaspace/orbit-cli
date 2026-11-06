package render_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/table"
)

func newPlain(buf *bytes.Buffer) *render.Printer {
	return render.NewPrinter(buf, render.WithPlain())
}

func TestTitleFollowedByEmptyLine(t *testing.T) {
	var buf bytes.Buffer
	newPlain(&buf).Title("orbit-cli")

	if got, want := buf.String(), "orbit-cli\n\n"; got != want {
		t.Errorf("Title output = %q, want %q", got, want)
	}
}

func TestNoTrailingWhitespace(t *testing.T) {
	var buf bytes.Buffer
	p := newPlain(&buf)
	p.Title("orbit-cli")
	p.Section("Checks")
	p.Line(render.StatusOK, "build")
	p.Line(render.StatusSkipped, "")
	p.KVs([2]string{"Name", "orbit-cli"}, [2]string{"Empty", ""})
	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: 3},
		render.SummaryPart{Status: render.StatusFail, Count: 0},
	)
	p.Hint("run orbit doctor --fix")
	p.Empty("pull requests")

	tbl := table.New(
		table.Column{Title: "NAME", MinWidth: 10},
		table.Column{Title: "STATUS", MinWidth: 12},
	).WithTerminalWidth(60)
	tbl.AddRow("orbit-cli", "ok")
	if err := p.Table(tbl); err != nil {
		t.Fatalf("Table: %v", err)
	}

	for i, line := range strings.Split(buf.String(), "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}
}

func TestKVAlignment(t *testing.T) {
	var buf bytes.Buffer
	newPlain(&buf).KVs(
		[2]string{"Name", "orbit-cli"},
		[2]string{"Version", "1.2.3"},
		[2]string{"Channel", ""},
	)

	want := "  Name     orbit-cli\n" +
		"  Version  1.2.3\n" +
		"  Channel\n"
	if got := buf.String(); got != want {
		t.Errorf("KVs output = %q, want %q", got, want)
	}
}

func TestKVBlockSeparatedFromBody(t *testing.T) {
	bodies := map[string]func(*render.Printer){
		"line": func(p *render.Printer) { p.Line(render.StatusOK, "fine") },
		"table": func(p *render.Printer) {
			tbl := table.New(
				table.Column{Title: "NAME", MinWidth: 10},
				table.Column{Title: "STATUS", MinWidth: 12},
			).WithTerminalWidth(60)
			tbl.AddRow("orbit-cli", "ok")
			_ = p.Table(tbl)
		},
		"empty":   func(p *render.Printer) { p.Empty("repositories") },
		"section": func(p *render.Printer) { p.Section("Checks") },
	}

	const prefix = "Subject\n\n  Scope  all\n  Mode   fast\n\n"
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			p := newPlain(&buf)
			p.Title("Subject")
			p.KVs([2]string{"Scope", "all"}, [2]string{"Mode", "fast"})
			body(p)
			if err := p.Err(); err != nil {
				t.Fatalf("render: %v", err)
			}

			out := buf.String()
			if !strings.HasPrefix(out, prefix) {
				t.Fatalf("output = %q, want the KV block followed by one empty line", out)
			}
			if strings.HasPrefix(strings.TrimPrefix(out, prefix), "\n") {
				t.Errorf("output = %q, want exactly one empty line after the KV block", out)
			}
		})
	}
}

func TestKVBlockThenSummaryKeepsOneBlankLine(t *testing.T) {
	var buf bytes.Buffer
	p := newPlain(&buf)
	p.Title("Subject")
	p.KVs([2]string{"Scope", "all"})
	p.Summary(render.SummaryPart{Status: render.StatusOK, Count: 1, Label: "ok"})

	want := "Subject\n\n  Scope  all\n\n[ok] 1 ok\n"
	if got := buf.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestSummaryKeepsZeroCounts(t *testing.T) {
	var buf bytes.Buffer
	newPlain(&buf).Summary(
		render.SummaryPart{Status: render.StatusOK, Count: 3, Label: "ok"},
		render.SummaryPart{Status: render.StatusWarn, Count: 1, Label: "warn"},
		render.SummaryPart{Status: render.StatusFail, Count: 0, Label: "fail"},
	)

	want := "\n[ok] 3 ok  [warn] 1 warn  [fail] 0 fail\n"
	if got := buf.String(); got != want {
		t.Errorf("Summary output = %q, want %q", got, want)
	}
}

func TestPlainModeUsesLabelsAndNoANSI(t *testing.T) {
	var buf bytes.Buffer
	p := newPlain(&buf)
	p.Line(render.StatusOK, "fine")
	p.Line(render.StatusWarn, "careful")
	p.Line(render.StatusFail, "broken")
	p.Section("Checks")

	out := buf.String()
	for _, want := range []string{"[ok]", "[warn]", "[fail]"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output %q does not contain %q", out, want)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain output contains ANSI: %q", out)
	}
}

func TestSectionWidth(t *testing.T) {
	var buf bytes.Buffer
	newPlain(&buf).Section("Checks")

	line := strings.TrimSuffix(buf.String(), "\n")
	if got := lipgloss.Width(line); got != 60 {
		t.Errorf("section width = %d, want 60: %q", got, line)
	}
	if !strings.HasPrefix(line, "── Checks ─") {
		t.Errorf("section line = %q, want the name in the rule", line)
	}
}

func TestSectionTruncatesLongName(t *testing.T) {
	var buf bytes.Buffer
	newPlain(&buf).Section(strings.Repeat("x", 100))

	line := strings.TrimSuffix(buf.String(), "\n")
	if got := lipgloss.Width(line); got != 60 {
		t.Errorf("section width = %d, want 60: %q", got, line)
	}
}

func TestSectionBlankLineOnlyAfterOutput(t *testing.T) {
	var buf bytes.Buffer
	p := newPlain(&buf)
	p.Section("First")
	p.Section("Second")

	lines := strings.Split(buf.String(), "\n")
	if len(lines) != 4 {
		t.Fatalf("Section output has %d lines, want 3 plus the trailing newline: %q", len(lines)-1, buf.String())
	}
	if lines[1] != "" {
		t.Errorf("separator line = %q, want empty", lines[1])
	}
	for _, i := range []int{0, 2} {
		if got := lipgloss.Width(lines[i]); got != 60 {
			t.Errorf("rule %d width = %d, want 60: %q", i, got, lines[i])
		}
	}
}

func TestSectionAfterTitleKeepsOneBlankLine(t *testing.T) {
	var buf bytes.Buffer
	p := newPlain(&buf)
	p.Title("System doctor")
	p.Section("System")

	want := "System doctor\n\n── System ─"
	if !strings.HasPrefix(buf.String(), want) {
		t.Errorf("Title followed by Section = %q, want one blank line then the rule", buf.String())
	}
}

func TestEmptyWording(t *testing.T) {
	var buf bytes.Buffer
	newPlain(&buf).Empty("pull requests")

	if got, want := buf.String(), "No pull requests found.\n"; got != want {
		t.Errorf("Empty output = %q, want %q", got, want)
	}
}

func TestHintIndented(t *testing.T) {
	var buf bytes.Buffer
	newPlain(&buf).Hint("run orbit doctor --fix")

	if got, want := buf.String(), "  hint: run orbit doctor --fix\n"; got != want {
		t.Errorf("Hint output = %q, want %q", got, want)
	}
}

// failingWriter fails after the given number of successful writes and returns
// the error at index calls.
type failingWriter struct {
	errs  []error
	calls int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.calls >= len(w.errs) {
		return len(p), nil
	}
	err := w.errs[w.calls]
	w.calls++
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func TestErrCapturesFirstWriteError(t *testing.T) {
	first := errors.New("first")
	second := errors.New("second")
	w := &failingWriter{errs: []error{first, second}}
	p := render.NewPrinter(w)

	p.Blank()
	p.Blank()

	if !errors.Is(p.Err(), first) {
		t.Errorf("Err() = %v, want the first write error", p.Err())
	}
	if w.calls != 1 {
		t.Errorf("writer calls = %d, want 1 after the first error", w.calls)
	}
}

func TestErrNilOnHealthyWriter(t *testing.T) {
	var buf bytes.Buffer
	p := newPlain(&buf)
	p.Title("orbit-cli")

	if err := p.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}
