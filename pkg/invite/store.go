package invite

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store manages persistent storage of invitations on disk.
type Store struct {
	mu       sync.RWMutex
	filePath string
}

// NewStore creates a new Store instance with a custom path or default path.
func NewStore(customPath string) (*Store, error) {
	p := customPath
	if p == "" {
		if envPath := os.Getenv("ORBIT_INVITES_FILE"); envPath != "" {
			p = envPath
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			p = filepath.Join(home, ".config", "orbit", "invites.json")
		}
	}
	return &Store{filePath: p}, nil
}

// FilePath returns the configured file path for this store.
func (s *Store) FilePath() string {
	return s.filePath
}

// SaveInvite inserts or updates an invite record in the store.
func (s *Store) SaveInvite(rec *InviteRecord) error {
	if rec == nil {
		return errors.New("cannot save nil invite record")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadRecordsLocked()
	if err != nil {
		return err
	}

	updated := false
	for i, r := range records {
		if r.ID == rec.ID || r.Token == rec.Token {
			records[i] = rec
			updated = true
			break
		}
	}
	if !updated {
		records = append(records, rec)
	}

	return s.saveRecordsLocked(records)
}

// ListInvites returns all stored invitations.
func (s *Store) ListInvites() ([]*InviteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadRecordsLocked()
}

// GetInvite finds an invitation by ID, ID prefix, or token.
func (s *Store) GetInvite(tokenOrID string) (*InviteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records, err := s.loadRecordsLocked()
	if err != nil {
		return nil, err
	}

	// 1. Exact match by ID or Token
	for _, r := range records {
		if r.ID == tokenOrID || r.Token == tokenOrID {
			return r, nil
		}
	}

	// 2. Prefix match on ID if tokenOrID is at least 6 characters
	if len(tokenOrID) >= 6 {
		var matches []*InviteRecord
		for _, r := range records {
			if strings.HasPrefix(r.ID, tokenOrID) {
				matches = append(matches, r)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("ambiguous invite ID prefix %q matches %d invitations", tokenOrID, len(matches))
		}
	}

	return nil, ErrInviteNotFound
}

// RevokeInvite marks an invitation as revoked by ID or token.
func (s *Store) RevokeInvite(tokenOrID string) (*InviteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadRecordsLocked()
	if err != nil {
		return nil, err
	}

	var target *InviteRecord
	// 1. Exact match
	for _, r := range records {
		if r.ID == tokenOrID || r.Token == tokenOrID {
			target = r
			break
		}
	}

	// 2. Prefix match
	if target == nil && len(tokenOrID) >= 6 {
		var matches []*InviteRecord
		for _, r := range records {
			if strings.HasPrefix(r.ID, tokenOrID) {
				matches = append(matches, r)
			}
		}
		if len(matches) == 1 {
			target = matches[0]
		} else if len(matches) > 1 {
			return nil, fmt.Errorf("ambiguous invite ID prefix %q matches %d invitations", tokenOrID, len(matches))
		}
	}

	if target == nil {
		return nil, ErrInviteNotFound
	}

	target.Revoked = true
	now := time.Now().UTC()
	target.RevokedAt = &now

	if err := s.saveRecordsLocked(records); err != nil {
		return nil, err
	}

	return target, nil
}

// RevokeAllInvites marks all non-revoked invitations in the store as revoked.
func (s *Store) RevokeAllInvites() ([]*InviteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadRecordsLocked()
	if err != nil {
		return nil, err
	}

	var revoked []*InviteRecord
	now := time.Now().UTC()
	for _, r := range records {
		if !r.Revoked {
			r.Revoked = true
			r.RevokedAt = &now
			revoked = append(revoked, r)
		}
	}

	if len(revoked) == 0 {
		return []*InviteRecord{}, nil
	}

	if err := s.saveRecordsLocked(records); err != nil {
		return nil, err
	}

	return revoked, nil
}


func (s *Store) loadRecordsLocked() ([]*InviteRecord, error) {
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return []*InviteRecord{}, nil
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, err
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return []*InviteRecord{}, nil
	}

	var records []*InviteRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("failed to parse invites file: %w", err)
	}

	return records, nil
}

func (s *Store) saveRecordsLocked(records []*InviteRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal invites: %w", err)
	}

	return atomicWriteInviteFile(s.filePath, data, 0600)
}

func atomicWriteInviteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-invites-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to atomic rename file to %q: %w", path, err)
	}

	return nil
}
