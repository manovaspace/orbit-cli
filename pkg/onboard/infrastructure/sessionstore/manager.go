// Package sessionstore persists onboarding checkpoints as a JSON file with
// atomic writes and 0600 permissions.
package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
)

// Manager persists one onboarding session on disk.
type Manager struct {
	filePath string
}

// New creates a Manager for a custom path, $ORBIT_SESSION_FILE, or
// ~/.config/orbit/session.json.
func New(customPath string) (*Manager, error) {
	if customPath != "" {
		return &Manager{filePath: customPath}, nil
	}
	if envPath := os.Getenv("ORBIT_SESSION_FILE"); envPath != "" {
		return &Manager{filePath: envPath}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Manager{
		filePath: filepath.Join(home, ".config", "orbit", "session.json"),
	}, nil
}

// FilePath returns the configured session file path.
func (m *Manager) FilePath() string {
	return m.filePath
}

// Create starts a new session. It is not written until Save is called.
func (m *Manager) Create(email, displayName string) *domain.SessionState {
	return domain.NewSession(email, displayName)
}

// HasPending reports whether a saved session exists and is not completed.
func (m *Manager) HasPending() bool {
	s, err := m.Load()
	if err != nil || s == nil {
		return false
	}
	return s.Pending()
}

// Load reads the session from disk. It returns nil, nil when no file exists.
func (m *Manager) Load() (*domain.SessionState, error) {
	if _, err := os.Stat(m.filePath); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return nil, err
	}

	var s domain.SessionState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}

	if s.Metadata == nil {
		s.Metadata = make(map[string]string)
	}
	if s.ClonedRepos == nil {
		s.ClonedRepos = make([]string, 0)
	}

	return &s, nil
}

// Save serializes the session to disk with an atomic write and 0600 file
// permissions.
func (m *Manager) Save(s *domain.SessionState) error {
	if s == nil {
		return errors.New("cannot save nil session")
	}

	if s.Metadata == nil {
		s.Metadata = make(map[string]string)
	}
	if s.ClonedRepos == nil {
		s.ClonedRepos = make([]string, 0)
	}

	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now

	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Atomic write via a temporary file in the same directory.
	tmpFile, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		if writeErr := os.WriteFile(m.filePath, data, 0600); writeErr != nil {
			return writeErr
		}
	} else {
		tmpPath := tmpFile.Name()
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if err := tmpFile.Chmod(0600); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to set temp file permissions: %w", err)
		}

		if _, err := tmpFile.Write(data); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to write session data: %w", err)
		}

		if err := tmpFile.Sync(); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to sync session data: %w", err)
		}

		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("failed to close temp file: %w", err)
		}

		if err := os.Rename(tmpPath, m.filePath); err != nil {
			return fmt.Errorf("failed to atomic rename session file: %w", err)
		}
	}

	return nil
}

// Clear removes the session file if it exists.
func (m *Manager) Clear() error {
	if err := os.Remove(m.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
