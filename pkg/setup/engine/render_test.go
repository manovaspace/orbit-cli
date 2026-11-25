package engine

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/render/rendertest"
)

func TestRenderMarksSudoAndBrowser(t *testing.T) {
	packages := fakePart{
		name: "packages",
		plan: func(context.Context, *State) (Plan, error) {
			noop := func(context.Context) error { return nil }
			return Plan{Actions: []Action{
				{Part: "packages", Summary: "install git", Sudo: true, Do: noop},
				{Part: "packages", Summary: "open keychain prompt", Sudo: true, Browser: true, Do: noop},
				{Part: "packages", Summary: "link ~/bin/orbit", Do: noop},
			}}, nil
		},
	}
	auth := fakePart{
		name:  "auth",
		needs: []string{"packages"},
		plan: func(context.Context, *State) (Plan, error) {
			return Plan{}, errors.New("gh is not logged in")
		},
	}
	ports := fakePart{
		name: "ports",
		plan: func(context.Context, *State) (Plan, error) {
			return Plan{Findings: []Finding{{
				Part:     "ports",
				Message:  "port 10000 is held by another process",
				Fix:      "stop the listener on 10000",
				Severity: SeverityError,
			}}}, nil
		},
	}
	handbook := fakePart{name: "handbook"}

	r := (&Engine{Parts: []Part{packages, auth, ports, handbook}}).Check(context.Background(), newState(t))

	var buf bytes.Buffer
	RenderPlan(render.NewPrinter(&buf, render.WithPlain()), r)
	rendertest.Golden(t, "plan_markers", buf.String())
}
