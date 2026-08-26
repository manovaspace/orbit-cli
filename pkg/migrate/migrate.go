package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultStateVersion defines the default migration state format version.
	DefaultStateVersion = "0.1.0"

	// DefaultStateRelativePath defines the standard relative location for state.json.
	DefaultStateRelativePath = ".orbit/state.json"
)

// Engine manages reading, writing, and executing workspace state migrations.
type Engine struct {
	workspaceRoot string
	statePath     string
}

// NewEngine creates a new migration Engine for the given workspace root.
// If statePath is empty, it defaults to <workspaceRoot>/.manova/state.json.
func NewEngine(workspaceRoot string, statePath string) *Engine {
	if statePath == "" {
		statePath = filepath.Join(workspaceRoot, DefaultStateRelativePath)
	}
	return &Engine{
		workspaceRoot: workspaceRoot,
		statePath:     statePath,
	}
}

// WorkspaceRoot returns the configured workspace root directory.
func (e *Engine) WorkspaceRoot() string {
	return e.workspaceRoot
}

// StatePath returns the path to the state.json file.
func (e *Engine) StatePath() string {
	return e.statePath
}

// LoadState reads the migration state from disk.
// If the state file does not exist, an empty initialized MigrationState is returned without error.
func (e *Engine) LoadState() (*MigrationState, error) {
	data, err := os.ReadFile(e.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &MigrationState{
				Version: DefaultStateVersion,
				Applied: []AppliedMigrationRecord{},
			}, nil
		}
		return nil, fmt.Errorf("failed to read migration state file %s: %w", e.statePath, err)
	}

	var state MigrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse migration state JSON from %s: %w", e.statePath, err)
	}

	if state.Version == "" {
		state.Version = DefaultStateVersion
	}
	if state.Applied == nil {
		state.Applied = []AppliedMigrationRecord{}
	}

	return &state, nil
}

// SaveState writes the migration state to disk formatted as indented JSON.
// It ensures that the parent directory exists before writing.
func (e *Engine) SaveState(state *MigrationState) error {
	if state == nil {
		return fmt.Errorf("cannot save nil migration state")
	}
	if state.Version == "" {
		state.Version = DefaultStateVersion
	}
	if state.Applied == nil {
		state.Applied = []AppliedMigrationRecord{}
	}

	dir := filepath.Dir(e.statePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for state file %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal migration state: %w", err)
	}

	data = append(data, '\n')
	if err := os.WriteFile(e.statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write migration state file %s: %w", e.statePath, err)
	}

	return nil
}

// Pending returns a list of migrations from the provided slice that have not yet been applied.
func (e *Engine) Pending(migrations []Migration) ([]Migration, error) {
	state, err := e.LoadState()
	if err != nil {
		return nil, err
	}

	appliedMap := make(map[string]bool, len(state.Applied))
	for _, record := range state.Applied {
		appliedMap[record.ID] = true
	}

	var pending []Migration
	for _, m := range migrations {
		if !appliedMap[m.ID] {
			pending = append(pending, m)
		}
	}

	return pending, nil
}

// Apply executes all pending migrations sequentially and records each successful migration in state.json.
// If any migration fails, execution halts and the error is returned along with the results up to the failure.
func (e *Engine) Apply(migrations []Migration) ([]MigrationResult, error) {
	state, err := e.LoadState()
	if err != nil {
		return nil, err
	}

	appliedMap := make(map[string]bool, len(state.Applied))
	for _, record := range state.Applied {
		appliedMap[record.ID] = true
	}

	var results []MigrationResult
	for _, m := range migrations {
		if appliedMap[m.ID] {
			continue
		}

		res := MigrationResult{
			ID:          m.ID,
			Description: m.Description,
		}

		if m.Run != nil {
			if err := m.Run(e.workspaceRoot); err != nil {
				res.Success = false
				res.Error = err.Error()
				results = append(results, res)
				return results, fmt.Errorf("migration %s failed: %w", m.ID, err)
			}
		}

		res.Success = true
		results = append(results, res)

		// Record in state and persist immediately
		state.Applied = append(state.Applied, AppliedMigrationRecord{
			ID:          m.ID,
			AppliedAt:   time.Now().UTC(),
			Description: m.Description,
		})
		appliedMap[m.ID] = true

		if err := e.SaveState(state); err != nil {
			return results, fmt.Errorf("failed to persist state after migration %s: %w", m.ID, err)
		}
	}

	return results, nil
}

// RunPendingMigrations executes all pending built-in migrations for the given workspace root.
func RunPendingMigrations(workspaceRoot string) ([]MigrationResult, error) {
	engine := NewEngine(workspaceRoot, "")
	return engine.Apply(GetBuiltinMigrations())
}
