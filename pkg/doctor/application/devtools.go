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

// devToolsCheck reports the agent tooling the workspace relies on: RTK (and
// whether its hook is installed) and opencode.
type devToolsCheck struct {
	probe Probe
}

func (c devToolsCheck) Name() string { return "dev-tools" }

func (c devToolsCheck) Run(ctx context.Context) []domain.DiagnosticResult {
	rtkOut, rtkErr := c.probe.Run(ctx, ShortCommandTimeout, "rtk", "--version")
	results := []domain.DiagnosticResult{domain.EvaluateRTK(rtkOut.Output, rtkErr)}
	if rtkErr == nil {
		gainOut, gainErr := c.probe.Run(ctx, DefaultCommandTimeout, "rtk", "gain")
		results = append(results, domain.EvaluateRTKHook(gainOut.Output, gainErr))
	}
	opencodeOut, opencodeErr := c.probe.Run(ctx, ShortCommandTimeout, "opencode", "--version")
	return append(results, domain.EvaluateOpenCode(opencodeOut.Output, opencodeErr))
}
