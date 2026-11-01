// Package application implements the workspace migration use cases over the
// state store port.
package application

import (
	"time"

	"github.com/manovaspace/orbit-cli/pkg/migrate/domain"
)

// StateStore persists the migration state document.
type StateStore interface {
	Load(path string) (*domain.MigrationState, error)
	Save(path string, state *domain.MigrationState) error
}

// Engine reads, writes and applies workspace migrations.
type Engine struct {
	WorkspaceRoot string
	StatePath     string
	Store         StateStore
	// Now defaults to time.Now.
	Now func() time.Time
}

// StatusEntry is one migration in the status listing.
type StatusEntry struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Applied     bool       `json:"applied"`
	AppliedAt   *time.Time `json:"appliedAt,omitempty"`
}

// StatusResult is what `orbit migrate status` produced.
type StatusResult struct {
	WorkspaceRoot string        `json:"workspaceRoot"`
	StatePath     string        `json:"statePath"`
	Applied       int           `json:"applied"`
	Pending       int           `json:"pending"`
	Entries       []StatusEntry `json:"entries"`
}

// ApplyResult is what `orbit migrate` produced.
type ApplyResult struct {
	WorkspaceRoot string                   `json:"workspaceRoot"`
	Results       []domain.MigrationResult `json:"results"`
	Applied       int                      `json:"applied"`
}
