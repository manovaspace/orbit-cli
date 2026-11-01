// Package statefile implements the migration state store over .orbit/migrations.json.
package statefile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/migrate/domain"
)

// Store reads and writes the migration state document.
type Store struct{}

// New builds a Store.
func New() *Store { return &Store{} }

// Load reads the state file. A missing file is an empty state with no error.
func (s *Store) Load(path string) (*domain.MigrationState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &domain.MigrationState{Version: domain.DefaultStateVersion, Applied: []domain.AppliedMigrationRecord{}}, nil
		}
		return nil, fmt.Errorf("failed to read migration state file %s: %w", path, err)
	}

	var state domain.MigrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse migration state JSON from %s: %w", path, err)
	}
	return &state, nil
}

// Save writes the state file as indented JSON, creating its directory.
func (s *Store) Save(path string, state *domain.MigrationState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory for state file %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal migration state: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write migration state file %s: %w", path, err)
	}
	return nil
}
