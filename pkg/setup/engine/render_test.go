package engine

import (
	"bytes"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/render/rendertest"
)

func TestRenderMarksSudoAndBrowser(t *testing.T) {
	r := Result{
		Parts: []PartResult{
			{
				Part:   "packages",
				Status: StatusPending,
				Actions: []string{
					"[sudo] install git",
					"[browser] [sudo] open keychain prompt",
					"link ~/bin/orbit",
				},
			},
			{
				Part:   "auth",
				Status: StatusWaits,
				Error:  "gh is not logged in",
			},
			{
				Part:   "ports",
				Status: StatusFailed,
				Findings: []Finding{{
					Part:     "ports",
					Message:  "port 10000 is held by another process",
					Fix:      "stop the listener on 10000",
					Severity: SeverityError,
				}},
			},
			{Part: "handbook", Status: StatusOK},
		},
		Changed: 1,
		Failed:  1,
	}

	var buf bytes.Buffer
	RenderPlan(render.NewPrinter(&buf, render.WithPlain()), r)
	rendertest.Golden(t, "plan_markers", buf.String())
}
