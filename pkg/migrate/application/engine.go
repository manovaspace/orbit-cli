package application

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/migrate/domain"
)

// NewEngine creates an Engine for workspaceRoot. An empty statePath defaults to
// <workspaceRoot>/.orbit/migrations.json.
func NewEngine(workspaceRoot, statePath string, store StateStore) *Engine {
	if statePath == "" {
		statePath = filepath.Join(workspaceRoot, domain.DefaultStateRelativePath)
	}
	return &Engine{WorkspaceRoot: workspaceRoot, StatePath: statePath, Store: store}
}

// LoadState reads the migration state and normalises its defaults.
func (e *Engine) LoadState() (*domain.MigrationState, error) {
	state, err := e.Store.Load(e.StatePath)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &domain.MigrationState{}
	}
	if state.Version == "" {
		state.Version = domain.DefaultStateVersion
	}
	if state.Applied == nil {
		state.Applied = []domain.AppliedMigrationRecord{}
	}
	return state, nil
}

// SaveState normalises and persists the migration state.
func (e *Engine) SaveState(state *domain.MigrationState) error {
	if state == nil {
		return fmt.Errorf("cannot save nil migration state")
	}
	if state.Version == "" {
		state.Version = domain.DefaultStateVersion
	}
	if state.Applied == nil {
		state.Applied = []domain.AppliedMigrationRecord{}
	}
	return e.Store.Save(e.StatePath, state)
}

// Pending returns the migrations in migrations that have not been applied.
func (e *Engine) Pending(migrations []domain.Migration) ([]domain.Migration, error) {
	state, err := e.LoadState()
	if err != nil {
		return nil, err
	}
	applied := make(map[string]bool, len(state.Applied))
	for _, record := range state.Applied {
		applied[record.ID] = true
	}

	var pending []domain.Migration
	for _, m := range migrations {
		if !applied[m.ID] {
			pending = append(pending, m)
		}
	}
	return pending, nil
}

// Apply executes pending migrations sequentially and records each success.
func (e *Engine) Apply(migrations []domain.Migration) ([]domain.MigrationResult, error) {
	state, err := e.LoadState()
	if err != nil {
		return nil, err
	}

	applied := make(map[string]bool, len(state.Applied))
	for _, record := range state.Applied {
		applied[record.ID] = true
	}

	var results []domain.MigrationResult
	for _, m := range migrations {
		if applied[m.ID] {
			continue
		}

		res := domain.MigrationResult{ID: m.ID, Description: m.Description}
		if m.Run != nil {
			if err := m.Run(e.WorkspaceRoot); err != nil {
				res.Success = false
				res.Error = err.Error()
				results = append(results, res)
				return results, fmt.Errorf("migration %s failed: %w", m.ID, err)
			}
		}
		res.Success = true
		results = append(results, res)

		state.Applied = append(state.Applied, domain.AppliedMigrationRecord{
			ID:          m.ID,
			AppliedAt:   e.now().UTC(),
			Description: m.Description,
		})
		applied[m.ID] = true

		if err := e.SaveState(state); err != nil {
			return results, fmt.Errorf("failed to persist state after migration %s: %w", m.ID, err)
		}
	}
	return results, nil
}

// RunPending applies every pending built-in migration.
func (e *Engine) RunPending() (ApplyResult, error) {
	results, err := e.Apply(domain.GetBuiltinMigrations())
	applied := 0
	for _, res := range results {
		if res.Success {
			applied++
		}
	}
	return ApplyResult{WorkspaceRoot: e.WorkspaceRoot, Results: results, Applied: applied}, err
}

// Status lists every built-in migration with its applied state.
func (e *Engine) Status() (StatusResult, error) {
	state, err := e.LoadState()
	if err != nil {
		return StatusResult{}, err
	}

	appliedAt := make(map[string]time.Time, len(state.Applied))
	for _, record := range state.Applied {
		appliedAt[record.ID] = record.AppliedAt
	}

	result := StatusResult{WorkspaceRoot: e.WorkspaceRoot, StatePath: e.StatePath, Entries: []StatusEntry{}}
	for _, m := range domain.GetBuiltinMigrations() {
		entry := StatusEntry{ID: m.ID, Description: m.Description}
		if at, ok := appliedAt[m.ID]; ok {
			entry.Applied = true
			entry.AppliedAt = &at
			result.Applied++
		} else {
			result.Pending++
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func (e *Engine) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}
