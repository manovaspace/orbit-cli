// Package doctorprobe adapts the doctor diagnostics and healer recipes to the
// onboarding flow.
package doctorprobe

import (
	"context"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/doctor/healer"
)

// Probe runs the pre-flight checks and their healing recipes.
type Probe struct {
	// RunDiagnostics defaults to doctor.RunDiagnostics.
	RunDiagnostics func() *doctor.DoctorReport
	// Registry defaults to the standard healing recipes.
	Registry func() *healer.Registry
}

// New builds a Probe over the default diagnostics and recipes.
func New() *Probe {
	return &Probe{}
}

// Run executes the pre-flight diagnostics.
func (p *Probe) Run() *doctor.DoctorReport {
	if p.RunDiagnostics != nil {
		return p.RunDiagnostics()
	}
	return doctor.RunDiagnostics()
}

// HealableCount reports how many results a healing recipe can remediate.
func (p *Probe) HealableCount(results []doctor.DiagnosticResult) int {
	return len(p.registry().FindHealers(results))
}

// Heal runs the matching healer recipes, reporting each name and status. The
// recipe errors are already reported through progress, as before.
func (p *Probe) Heal(ctx context.Context, results []doctor.DiagnosticResult, progress func(name, status string)) error {
	_, _ = healer.RunHealers(ctx, results, progress)
	return nil
}

func (p *Probe) registry() *healer.Registry {
	if p.Registry != nil {
		return p.Registry()
	}
	return healer.NewDefaultRegistry()
}
