package render_test

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/manovaspace/orbit-cli/pkg/render"
)

func TestStatusTable(t *testing.T) {
	tests := []struct {
		status render.Status
		label  string
		glyph  string
		style  lipgloss.Style
	}{
		{render.StatusOK, "ok", "✔", render.Success},
		{render.StatusWarn, "warn", "⚠", render.Warning},
		{render.StatusFail, "fail", "✖", render.Error},
		{render.StatusInfo, "info", "ℹ", render.Info},
		{render.StatusSkipped, "skipped", "○", render.Subtle},
		{render.StatusUnverified, "unverified", "?", render.Warning},
		{render.Status(99), "info", "ℹ", render.Info},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if got := tt.status.Label(); got != tt.label {
				t.Errorf("Label() = %q, want %q", got, tt.label)
			}
			if got := tt.status.Icon(); got != tt.glyph {
				t.Errorf("Icon() = %q, want %q", got, tt.glyph)
			}
			if got := tt.status.Style(); !reflect.DeepEqual(got, tt.style) {
				t.Errorf("Style() = %+v, want %+v", got, tt.style)
			}
		})
	}
}
