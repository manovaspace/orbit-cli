package owner

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// GenerateMasterSecret generates a 32-byte cryptographically secure random secret string (64 hex characters).
func GenerateMasterSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate master secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ComputeFingerprint calculates a SHA-256 fingerprint representation of a secret.
func ComputeFingerprint(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:16])
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
func (s *Store) SaveOwner(rec *OwnerRecord) error {
	if rec == nil {
		return ErrInvalidOwnerRecord
	}
	if strings.TrimSpace(rec.Email) == "" || strings.TrimSpace(rec.RootSigningSecret) == "" {
		return ErrInvalidOwnerRecord
	}

	if rec.VerifiedAt.IsZero() {
		rec.VerifiedAt = time.Now().UTC()
	}

	if rec.KeyFingerprint == "" {
		rec.KeyFingerprint = ComputeFingerprint(rec.RootSigningSecret)
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

	if err := os.WriteFile(s.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write owner file: %w", err)
	}

	// Ensure permissions remain 0600 even if file previously existed with wider permissions
	if err := os.Chmod(s.filePath, 0600); err != nil {
		return fmt.Errorf("failed to set 0600 permissions on owner file: %w", err)
	}

	return nil
}

// LoadOwner reads and parses the OwnerRecord from disk.
// Returns ErrOwnerNotFound if the file does not exist or is empty.
func (s *Store) LoadOwner() (*OwnerRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return nil, ErrOwnerNotFound
	} else if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read owner file: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, ErrOwnerNotFound
	}

	var rec OwnerRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("failed to parse owner record: %w", err)
	}

	return &rec, nil
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
		return ErrOwnerNotFound
	} else if err != nil {
		return err
	}

	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		return ErrInsecurePermissions
	}

	return nil
}
