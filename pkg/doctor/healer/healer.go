package healer

import (
	"context"
	"fmt"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
)

// NewDefaultRegistry creates and populates a Registry with all standard Orbit healers:
// Go 1.24, Bun, Node.js 22 LTS, Git, and Docker Compose.
func NewDefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(NewGoHealer())
	reg.Register(NewBunHealer())
	reg.Register(NewNodeHealer())
	reg.Register(NewGitHealer())
	reg.Register(NewDockerComposeHealer())
	return reg
}

// Run executes all matching healers against the provided diagnostic results.
// It executes healers sequentially with error isolation: if one healer fails,
// it records the failure and proceeds with the remaining healers.
func (r *Registry) Run(ctx context.Context, results []doctor.DiagnosticResult, progress func(name, status string)) ([]HealResult, error) {
	if progress == nil {
		progress = func(name, status string) {}
	}

	healers := r.FindHealers(results)
	if len(healers) == 0 {
		return []HealResult{}, nil
	}

	var healResults []HealResult

	for _, h := range healers {
		if err := ctx.Err(); err != nil {
			return healResults, err
		}

		start := time.Now()
		progress(h.Name(), "Starting auto-heal...")

		err := h.Heal(ctx, func(step string) {
			progress(h.Name(), step)
		})

		duration := time.Since(start)

		if err != nil {
			progress(h.Name(), fmt.Sprintf("Failed: %v", err))
			healResults = append(healResults, HealResult{
				HealerName: h.Name(),
				Success:    false,
				Message:    fmt.Sprintf("Failed to heal %s: %v", h.Name(), err),
				Error:      err,
				Duration:   duration,
			})
		} else {
			progress(h.Name(), "Completed successfully")
			healResults = append(healResults, HealResult{
				HealerName: h.Name(),
				Success:    true,
				Message:    fmt.Sprintf("Successfully installed/configured %s", h.Name()),
				Duration:   duration,
			})
		}
	}

	return healResults, nil
}

// RunHealers is a package-level helper that invokes the default registry against diagnostic results.
func RunHealers(ctx context.Context, results []doctor.DiagnosticResult, progress func(name, status string)) ([]HealResult, error) {
	reg := NewDefaultRegistry()
	return reg.Run(ctx, results, progress)
}

// IsAutoHealable returns true if any default healer can remediate the given diagnostic result.
func IsAutoHealable(result doctor.DiagnosticResult) bool {
	reg := NewDefaultRegistry()
	_, found := reg.FindHealer(result)
	return found
}
