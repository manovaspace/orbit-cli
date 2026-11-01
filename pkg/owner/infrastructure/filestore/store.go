package filestore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner/domain"
)

// DefaultStorePath returns the standard location of the owner.json vault file.
// Resolution order:
// 1. $ORBIT_OWNER_STORE environment variable
// 2. ~/.config/orbit/owner.json (user home directory)
// 3. /etc/orbit/owner.json (system fallback)
func DefaultStorePath() string {
	if envPath := os.Getenv("ORBIT_OWNER_STORE"); envPath != "" {
		return envPath
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".config", "orbit", "owner.json")
	}

	return "/etc/orbit/owner.json"
}

// Store manages persistent storage and cryptographic sealing of owner records on disk.
type Store struct {
	mu       sync.RWMutex
	filePath string
}

// NewStore creates a new Store instance with a custom or default path.
func NewStore(customPath string) *Store {
	p := customPath
	if p == "" {
		p = DefaultStorePath()
	}
	return &Store{filePath: p}
}

// FilePath returns the configured file path for this store.
func (s *Store) FilePath() string {
	return s.filePath
}

// SaveOwner writes the OwnerRecord to disk with POSIX 0600 permissions.
// Automatically creates parent directories (0700) if they don't exist.
func (s *Store) SaveOwner(rec *domain.OwnerRecord) error {
	if rec == nil {
		return domain.ErrInvalidOwnerRecord
	}
	if strings.TrimSpace(rec.Email) == "" || strings.TrimSpace(rec.RootSigningSecret) == "" {
		return domain.ErrInvalidOwnerRecord
	}

	if rec.VerifiedAt.IsZero() {
		rec.VerifiedAt = time.Now().UTC()
	}

	if rec.KeyFingerprint == "" {
		rec.KeyFingerprint = domain.ComputeFingerprint(rec.RootSigningSecret)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create owner directory %q: %w", dir, err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal owner record: %w", err)
	}

	return atomicWriteOwnerFile(s.filePath, data, 0600)
}

func atomicWriteOwnerFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-owner-*")
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

// LoadOwner reads and parses the OwnerRecord from disk.
// Returns domain.ErrOwnerNotFound if the file does not exist or is empty.
func (s *Store) LoadOwner() (*domain.OwnerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return nil, domain.ErrOwnerNotFound
	} else if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read owner file: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, domain.ErrOwnerNotFound
	}

	var rec domain.OwnerRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("failed to parse owner record: %w", err)
	}

	return &rec, nil
}

// Exists reports whether the vault file is present on disk.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.filePath)
	return err == nil
}

// IsVerified returns true if an owner record exists and has completed verification.
func (s *Store) IsVerified() bool {
	rec, err := s.LoadOwner()
	if err != nil || rec == nil {
		return false
	}
	return rec.IsVerified()
}

// CheckPermissions verifies that the owner vault file has secure permissions (mode 0600, no group/other access).
func (s *Store) CheckPermissions() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := os.Stat(s.filePath)
	if os.IsNotExist(err) {
		return domain.ErrOwnerNotFound
	} else if err != nil {
		return err
	}

	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		return domain.ErrInsecurePermissions
	}

	return nil
}
