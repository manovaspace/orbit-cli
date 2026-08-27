package healer

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
)

// Healer defines the contract for auto-remediation recipes that fix failed or warning diagnostic checks.
type Healer interface {
	// Name returns the human-readable identifier of the healer.
	Name() string
	// CanHeal evaluates if this healer can fix the reported diagnostic issue.
	CanHeal(result doctor.DiagnosticResult) bool
	// Heal executes the auto-remediation recipe, reporting incremental steps via progress callback.
	Heal(ctx context.Context, progress func(step string)) error
}

// HealResult represents the outcome of an attempted auto-healing action.
type HealResult struct {
	HealerName string        `json:"healer_name" yaml:"healer_name"`
	Success    bool          `json:"success" yaml:"success"`
	Message    string        `json:"message" yaml:"message"`
	Error      error         `json:"error,omitempty" yaml:"error,omitempty"`
	Duration   time.Duration `json:"duration" yaml:"duration"`
}

// Runner abstracts process and shell command execution for testability and portability.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
	RunShell(ctx context.Context, script string) (string, error)
}

// DefaultRunner executes real commands via os/exec.
type DefaultRunner struct{}

// Run executes a binary directly with arguments.
func (r *DefaultRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RunShell executes a shell command string in bash.
func (r *DefaultRunner) RunShell(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Registry manages a thread-safe collection of registered Healer implementations.
type Registry struct {
	mu      sync.RWMutex
	healers []Healer
}

// NewRegistry creates a new empty healer registry.
func NewRegistry() *Registry {
	return &Registry{
		healers: make([]Healer, 0),
	}
}

// Register adds a healer to the registry.
func (r *Registry) Register(h Healer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healers = append(r.healers, h)
}

// All returns a slice containing all registered healers.
func (r *Registry) All() []Healer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Healer, len(r.healers))
	copy(out, r.healers)
	return out
}

// Get returns a healer by name if registered.
func (r *Registry) Get(name string) (Healer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, h := range r.healers {
		if h.Name() == name {
			return h, true
		}
	}
	return nil, false
}

// FindHealer returns the first registered healer capable of healing the given diagnostic result.
func (r *Registry) FindHealer(result doctor.DiagnosticResult) (Healer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if result.Status == doctor.StatusOK {
		return nil, false
	}
	for _, h := range r.healers {
		if h.CanHeal(result) {
			return h, true
		}
	}
	return nil, false
}

// FindHealers returns a deduplicated slice of healers capable of healing the given diagnostic results.
func (r *Registry) FindHealers(results []doctor.DiagnosticResult) []Healer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matched []Healer
	seen := make(map[string]bool)

	for _, res := range results {
		if res.Status == doctor.StatusOK {
			continue
		}
		for _, h := range r.healers {
			if h.CanHeal(res) && !seen[h.Name()] {
				seen[h.Name()] = true
				matched = append(matched, h)
			}
		}
	}
	return matched
}
