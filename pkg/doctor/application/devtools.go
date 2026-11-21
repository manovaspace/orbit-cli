package application

import (
	"context"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
)

// resourcesCheck warns when free disk or available memory cannot run the
// workspace. Values come from the host port so tests inject them.
type resourcesCheck struct {
	host Host
}

func (c resourcesCheck) Name() string { return "resources" }

func (c resourcesCheck) Run(context.Context) []domain.DiagnosticResult {
	return domain.EvaluateResources(c.host.Resources())
}

// devToolsCheck reports the agent runtime the workspace relies on: opencode.
type devToolsCheck struct {
	probe Probe
}

func (c devToolsCheck) Name() string { return "dev-tools" }

func (c devToolsCheck) Run(ctx context.Context) []domain.DiagnosticResult {
	opencodeOut, opencodeErr := c.probe.Run(ctx, ShortCommandTimeout, "opencode", "--version")
	return []domain.DiagnosticResult{domain.EvaluateOpenCode(opencodeOut.Output, opencodeErr)}
}
