package domain

import (
	"context"
	"sync"
	"time"
)

// Healer defines the contract for auto-remediation recipes that fix failed or
// warning diagnostic checks.
type Healer interface {
	// Name returns the human-readable identifier of the healer.
	Name() string
	// CanHeal evaluates if this healer can fix the reported diagnostic issue.
	CanHeal(result DiagnosticResult) bool
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

// HealerRegistry holds the remediation recipes that may run under --fix.
type HealerRegistry struct {
	mu      sync.RWMutex
	healers []Healer
}

// NewHealerRegistry creates a new empty healer registry.
func NewHealerRegistry() *HealerRegistry {
	return &HealerRegistry{healers: make([]Healer, 0)}
}

// Register adds a healer to the registry.
func (r *HealerRegistry) Register(h Healer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healers = append(r.healers, h)
}

// All returns a slice containing all registered healers.
func (r *HealerRegistry) All() []Healer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Healer, len(r.healers))
	copy(out, r.healers)
	return out
}

// Get returns a healer by name if registered.
func (r *HealerRegistry) Get(name string) (Healer, bool) {
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
func (r *HealerRegistry) FindHealer(result DiagnosticResult) (Healer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if result.Status == StatusOK || result.Status == StatusUnverified {
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
func (r *HealerRegistry) FindHealers(results []DiagnosticResult) []Healer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matched []Healer
	seen := make(map[string]bool)

	for _, res := range results {
		if res.Status == StatusOK || res.Status == StatusUnverified {
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
