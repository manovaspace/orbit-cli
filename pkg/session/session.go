package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SessionManager manages checkpointed onboarding sessions on disk.
type SessionManager struct {
	filePath   string
	legacyPath string
}

// NewSessionManager creates a SessionManager instance with either a custom path
// or ~/.config/orbit/session.json. A leftover ~/.config/manova/session.json is
// still read until the next save, which writes the orbit path.
func NewSessionManager(customPath string) (*SessionManager, error) {
	if customPath != "" {
		return &SessionManager{filePath: customPath}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &SessionManager{
		filePath:   filepath.Join(home, ".config", "orbit", "session.json"),
		legacyPath: filepath.Join(home, ".config", "manova", "session.json"),
	}, nil
}

// FilePath returns the configured session file path.
func (sm *SessionManager) FilePath() string {
	return sm.filePath
}

// CreateSession generates a new onboarding session with an 8-byte hex ID.
func (sm *SessionManager) CreateSession(email, displayName string) *Session {
	rawID := make([]byte, 8)
	_, _ = rand.Read(rawID)
	now := time.Now().UTC()

	return &Session{
		ID:           hex.EncodeToString(rawID),
		Email:        email,
		DisplayName:  displayName,
		CurrentStage: StageInit,
		ClonedRepos:  make([]string, 0),
		Metadata:     make(map[string]string),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// HasPendingSession checks whether a saved session exists and is not yet completed.
func (sm *SessionManager) HasPendingSession() bool {
	s, err := sm.LoadSession()
	if err != nil || s == nil {
		return false
	}
	return s.CurrentStage != StageCompleted
}

func (sm *SessionManager) readPath() string {
	if _, err := os.Stat(sm.filePath); err == nil {
		return sm.filePath
	}
	if sm.legacyPath != "" {
		if _, err := os.Stat(sm.legacyPath); err == nil {
			return sm.legacyPath
		}
	}
	return sm.filePath
}

// LoadSession reads and unmarshals the session from disk.
// Returns nil, nil if the file does not exist.
func (sm *SessionManager) LoadSession() (*Session, error) {
	path := sm.readPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}

	return &s, nil
}

// SaveSession serializes the session to disk with 0600 file permissions.
func (sm *SessionManager) SaveSession(s *Session) error {
	if s == nil {
		return errors.New("cannot save nil session")
	}

	s.UpdatedAt = time.Now().UTC()
	dir := filepath.Dir(sm.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	if err := os.WriteFile(sm.filePath, data, 0600); err != nil {
		return err
	}
	if sm.legacyPath != "" && sm.legacyPath != sm.filePath {
		_ = os.Remove(sm.legacyPath)
	}
	return nil
}

// ClearSession removes the session file if it exists.
func (sm *SessionManager) ClearSession() error {
	if err := os.Remove(sm.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if sm.legacyPath != "" {
		if err := os.Remove(sm.legacyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
