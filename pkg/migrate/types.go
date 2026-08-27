package migrate

import "time"

// Migration defines a single versioned workspace migration step.
type Migration struct {
	ID          string                           `json:"id" yaml:"id"`
	Description string                           `json:"description" yaml:"description"`
	Run         func(workspaceRoot string) error `json:"-" yaml:"-"`
}

// AppliedMigrationRecord records metadata for an applied migration in state.json.
type AppliedMigrationRecord struct {
	ID          string    `json:"id" yaml:"id"`
	AppliedAt   time.Time `json:"applied_at" yaml:"applied_at"`
	Description string    `json:"description" yaml:"description"`
}

// MigrationState represents the persisted state in .manova/state.json.
type MigrationState struct {
	Version string                   `json:"version" yaml:"version"`
	Applied []AppliedMigrationRecord `json:"applied" yaml:"applied"`
}

// IsApplied returns true if a migration with the given ID has already been applied.
func (s *MigrationState) IsApplied(id string) bool {
	if s == nil {
		return false
	}
	for _, a := range s.Applied {
		if a.ID == id {
			return true
		}
	}
	return false
}

// MigrationResult captures the outcome of applying a single migration step.
type MigrationResult struct {
	ID          string `json:"id" yaml:"id"`
	Description string `json:"description" yaml:"description"`
	Success     bool   `json:"success" yaml:"success"`
	Error       string `json:"error,omitempty" yaml:"error,omitempty"`
	Skipped     bool   `json:"skipped" yaml:"skipped"`
}
