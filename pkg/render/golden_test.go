package render_test

import (
	"bytes"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/render/rendertest"
	"github.com/manovaspace/orbit-cli/pkg/table"
)

func TestPrinterGoldens(t *testing.T) {
	t.Run("listing", func(t *testing.T) {
		var buf bytes.Buffer
		p := newPlain(&buf)
		p.Title("Open pull requests — orbit-cli")

		tbl := table.New(
			table.Column{Title: "NUMBER", MinWidth: 6},
			table.Column{Title: "TITLE", Flexible: true},
			table.Column{Title: "BRANCH", MinWidth: 12},
		).WithTerminalWidth(60)
		tbl.AddRow("#42", "Add the render package", "feat/render-package")
		tbl.AddRow("#41", "Fix doctor exit code", "fix/doctor-exit")
		if err := p.Table(tbl); err != nil {
			t.Fatalf("Table: %v", err)
		}
		p.Summary(render.SummaryPart{Status: render.StatusOK, Count: 2, Label: "open"})

		rendertest.Golden(t, "listing", buf.String())
	})

	t.Run("findings", func(t *testing.T) {
		var buf bytes.Buffer
		p := newPlain(&buf)
		p.Section("Agents")
		p.Line(render.StatusOK, "claude CLI present")
		p.Line(render.StatusWarn, "cursor CLI not installed")
		p.Line(render.StatusFail, "opencode CLI missing")
		p.Section("Environment")
		p.KVs(
			[2]string{"Workspace", "/home/orbit"},
			[2]string{"Contracts", "3 checked"},
		)
		p.Summary(
			render.SummaryPart{Status: render.StatusOK, Count: 1},
			render.SummaryPart{Status: render.StatusWarn, Count: 1},
			render.SummaryPart{Status: render.StatusFail, Count: 1},
			render.SummaryPart{Status: render.StatusSkipped, Count: 0},
			render.SummaryPart{Status: render.StatusUnverified, Count: 2},
		)

		rendertest.Golden(t, "findings", buf.String())
	})

	t.Run("error_hint", func(t *testing.T) {
		var buf bytes.Buffer
		p := newPlain(&buf)
		p.Title("Apply migrations")
		p.Line(render.StatusFail, "migration 0042 failed: relation already exists")
		p.Hint("run orbit migrate status, then orbit doctor --fix")

		rendertest.Golden(t, "error_hint", buf.String())
	})

	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		p := newPlain(&buf)
		p.Title("Open pull requests")
		p.Empty("pull requests")

		rendertest.Golden(t, "empty", buf.String())
	})
}
