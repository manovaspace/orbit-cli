// Package healer keeps the default remediation recipes reachable for callers
// outside the doctor command group. The recipes live in the layered
// infrastructure package.
package healer

import (
	"context"

	"github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/doctor/infrastructure/recipes"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Healer is one remediation recipe.
type Healer = domain.Healer

// HealResult is the outcome of one recipe.
type HealResult = domain.HealResult

// Registry holds remediation recipes.
type Registry = domain.HealerRegistry

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return domain.NewHealerRegistry() }

// NewDefaultRegistry creates the standard Orbit recipes over the real runner.
func NewDefaultRegistry() *Registry {
	return recipes.NewDefaultRegistry(runner.Exec{})
}

// RunHealers executes the matching default recipes with error isolation.
func RunHealers(ctx context.Context, results []domain.DiagnosticResult, progress func(name, status string)) ([]HealResult, error) {
	reg := NewDefaultRegistry()
	return application.RunHealers(ctx, reg.FindHealers(results), progress)
}

// IsAutoHealable returns true if any default healer can remediate the given diagnostic result.
func IsAutoHealable(result domain.DiagnosticResult) bool {
	reg := NewDefaultRegistry()
	_, found := reg.FindHealer(result)
	return found
}
