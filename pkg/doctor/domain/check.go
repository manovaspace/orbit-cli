package domain

import "context"

// Check is one diagnostic probe. A new check joins the default suite by
// implementing this interface and registering one line in the application
// wiring.
type Check interface {
	// Name identifies the check in the registry.
	Name() string
	// Run performs the probe and returns its results, in display order.
	Run(ctx context.Context) []DiagnosticResult
}

// Registry holds the checks that make up one diagnostics run.
type Registry struct {
	checks []Check
}

// NewRegistry creates an empty check registry.
func NewRegistry() *Registry { return &Registry{} }

// Register appends a check to the run.
func (r *Registry) Register(check Check) {
	r.checks = append(r.checks, check)
}

// All returns the registered checks in order.
func (r *Registry) All() []Check {
	out := make([]Check, len(r.checks))
	copy(out, r.checks)
	return out
}

// Run executes every registered check and aggregates the results.
func (r *Registry) Run(ctx context.Context) *DoctorReport {
	report := NewReport()
	for _, check := range r.checks {
		report.AddAll(check.Run(ctx))
	}
	return report
}
