package owner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultGrantTTL is the standard lifetime of an admin grant (15 minutes).
	DefaultGrantTTL = 15 * time.Minute

	// DefaultGrantRole is the standard role assigned to a new admin.
	DefaultGrantRole = "admin"

	// DefaultGrantMaxAttempts is the maximum number of failed verification attempts before invalidation.
	DefaultGrantMaxAttempts = 3
)

var (
	// ErrGrantNotFound indicates that no active grant exists for the given email.
	ErrGrantNotFound = errors.New("no active admin grant found for email")

	// ErrGrantExpired indicates that the admin grant has passed its expiration time.
	ErrGrantExpired = errors.New("admin grant code has expired")

	// ErrGrantAlreadyUsed indicates that the admin grant has already been consumed.
	ErrGrantAlreadyUsed = errors.New("admin grant code has already been consumed")

	// ErrGrantMaxAttempts indicates that the maximum verification attempts have been exceeded.
	ErrGrantMaxAttempts = errors.New("maximum grant verification attempts exceeded")

	// ErrInvalidGrantCode indicates that the submitted code does not match the active grant.
	ErrInvalidGrantCode = errors.New("invalid admin grant code")
)

// GrantRecord represents an owner-issued one-time administrative authorization grant.
type GrantRecord struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	CodeHash    string     `json:"code_hash"`
	Salt        string     `json:"salt"`
	Role        string     `json:"role"`
	IssuedAt    time.Time  `json:"issued_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CreatedBy   string     `json:"created_by"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	Used        bool       `json:"used"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
}

// AdminGrant is an alias for GrantRecord.
type AdminGrant = GrantRecord

// IsExpired returns true if the grant has passed its expiration timestamp.
func (g *GrantRecord) IsExpired() bool {
	if g == nil || g.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().UTC().After(g.ExpiresAt)
}

// GrantStore defines the persistence interface required by GrantManager.
type GrantStore interface {
	SaveGrant(ctx context.Context, g *AdminGrant) error
	GetGrant(ctx context.Context, email, codeHash string) (*AdminGrant, error)
	MarkUsed(ctx context.Context, id string) error
	ListActiveGrants(ctx context.Context) ([]*AdminGrant, error)
}

// Generate8DigitCode generates a cryptographically secure 8-digit numeric OTP string (10000000-99999999).
func Generate8DigitCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(90000000))
	if err != nil {
		return "", fmt.Errorf("failed to generate secure 8-digit code: %w", err)
	}
	raw := fmt.Sprintf("%08d", n.Int64()+10000000)
	return Format8DigitCode(raw), nil
}

// Format8DigitCode formats an 8-digit string into standard XXXX-XXXX presentation format.
func Format8DigitCode(code string) string {
	clean := CleanCode(code)
	if len(clean) == 8 {
		return clean[:4] + "-" + clean[4:]
	}
	return clean
}

// CleanCode removes hyphens, spaces, and formatting characters from a code string.
func CleanCode(code string) string {
	clean := strings.ReplaceAll(code, "-", "")
	clean = strings.ReplaceAll(clean, " ", "")
	return strings.TrimSpace(clean)
}

// HashGrantCode computes a salted SHA-256 hash for a grant code.
func HashGrantCode(code, salt string) string {
	clean := CleanCode(code)
	h := sha256.Sum256([]byte(salt + ":" + clean))
	return hex.EncodeToString(h[:])
}

// GrantManager manages active admin grants with thread-safe access (in-memory or persistent).
type GrantManager struct {
	mu     sync.RWMutex
	grants map[string]*GrantRecord // keyed by normalized email
	store  GrantStore
}

// NewGrantManager creates a new in-memory GrantManager instance.
func NewGrantManager() *GrantManager {
	return &GrantManager{
		grants: make(map[string]*GrantRecord),
	}
}

// NewPersistentGrantManager creates a GrantManager backed by a persistent store.
func NewPersistentGrantManager(store GrantStore) *GrantManager {
	return &GrantManager{
		grants: make(map[string]*GrantRecord),
		store:  store,
	}
}

// WithStore configures a persistent GrantStore on the manager.
func (m *GrantManager) WithStore(store GrantStore) *GrantManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
	return m
}

// Store returns the configured GrantStore or nil if operating in-memory.
func (m *GrantManager) Store() GrantStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store
}

// CreateGrant generates and registers an 8-digit admin grant for the specified email.
func (m *GrantManager) CreateGrant(email, role, createdBy string, ttl time.Duration) (*GrantRecord, string, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return nil, "", errors.New("email cannot be empty")
	}

	if !strings.Contains(normEmail, "@") || !strings.Contains(normEmail, ".") {
		return nil, "", fmt.Errorf("invalid email address %q", email)
	}

	if ttl <= 0 {
		ttl = DefaultGrantTTL
	}
	if role == "" {
		role = DefaultGrantRole
	}

	codeFormatted, err := Generate8DigitCode()
	if err != nil {
		return nil, "", err
	}

	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return nil, "", fmt.Errorf("failed to generate grant ID: %w", err)
	}
	grantID := hex.EncodeToString(rawID)

	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return nil, "", fmt.Errorf("failed to generate grant salt: %w", err)
	}
	salt := hex.EncodeToString(saltBytes)

	codeHash := HashGrantCode(codeFormatted, salt)
	now := time.Now().UTC()

	rec := &GrantRecord{
		ID:          grantID,
		Email:       normEmail,
		CodeHash:    codeHash,
		Salt:        salt,
		Role:        role,
		IssuedAt:    now,
		ExpiresAt:   now.Add(ttl),
		CreatedBy:   createdBy,
		Attempts:    0,
		MaxAttempts: DefaultGrantMaxAttempts,
		Used:        false,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveGrant(context.Background(), rec); err != nil {
			return nil, "", err
		}
	}
	m.grants[normEmail] = rec

	return rec, codeFormatted, nil
}

// RegisterGrantWithCode allows registering an explicit grant with a pre-set code (useful for deterministic tests or CLI relays).
func (m *GrantManager) RegisterGrantWithCode(email, role, code, createdBy string, ttl time.Duration) (*GrantRecord, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return nil, errors.New("email cannot be empty")
	}

	clean := CleanCode(code)
	if len(clean) != 8 {
		return nil, fmt.Errorf("grant code must be exactly 8 numeric digits, got %d digits", len(clean))
	}

	if ttl <= 0 {
		ttl = DefaultGrantTTL
	}
	if role == "" {
		role = DefaultGrantRole
	}

	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return nil, fmt.Errorf("failed to generate grant ID: %w", err)
	}
	grantID := hex.EncodeToString(rawID)

	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return nil, fmt.Errorf("failed to generate grant salt: %w", err)
	}
	salt := hex.EncodeToString(saltBytes)

	codeHash := HashGrantCode(clean, salt)
	now := time.Now().UTC()

	rec := &GrantRecord{
		ID:          grantID,
		Email:       normEmail,
		CodeHash:    codeHash,
		Salt:        salt,
		Role:        role,
		IssuedAt:    now,
		ExpiresAt:   now.Add(ttl),
		CreatedBy:   createdBy,
		Attempts:    0,
		MaxAttempts: DefaultGrantMaxAttempts,
		Used:        false,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveGrant(context.Background(), rec); err != nil {
			return nil, err
		}
	}
	m.grants[normEmail] = rec

	return rec, nil
}

// VerifyGrant validates the submitted 8-digit code against the active grant for the email.
// If valid, the grant is marked as Used and returns the record.
// If invalid, attempts are incremented; exceeding MaxAttempts purges the grant.
func (m *GrantManager) VerifyGrant(email, submittedCode string) (*GrantRecord, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	cleanCode := CleanCode(submittedCode)

	if normEmail == "" {
		return nil, errors.New("email cannot be empty")
	}
	if cleanCode == "" {
		return nil, errors.New("code cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.store != nil {
		rec, err := m.store.GetGrant(context.Background(), normEmail, "")
		if err != nil {
			if errors.Is(err, ErrGrantNotFound) {
				return nil, ErrGrantNotFound
			}
			return nil, err
		}
		if rec == nil {
			return nil, ErrGrantNotFound
		}

		if rec.Used {
			return nil, ErrGrantAlreadyUsed
		}

		if rec.IsExpired() {
			delete(m.grants, normEmail)
			return nil, ErrGrantExpired
		}

		rec.Attempts++

		expectedHash := HashGrantCode(cleanCode, rec.Salt)
		if subtle.ConstantTimeCompare([]byte(rec.CodeHash), []byte(expectedHash)) != 1 {
			if rec.Attempts >= rec.MaxAttempts {
				_ = m.store.MarkUsed(context.Background(), rec.ID)
				delete(m.grants, normEmail)
				return nil, ErrGrantMaxAttempts
			}
			return nil, ErrInvalidGrantCode
		}

		// Code is valid! Mark used.
		if err := m.store.MarkUsed(context.Background(), rec.ID); err != nil {
			return nil, err
		}
		rec.Used = true
		now := time.Now().UTC()
		rec.UsedAt = &now
		delete(m.grants, normEmail)

		recCopy := *rec
		return &recCopy, nil
	}

	rec, exists := m.grants[normEmail]
	if !exists {
		return nil, ErrGrantNotFound
	}

	if rec.Used {
		return nil, ErrGrantAlreadyUsed
	}

	if rec.IsExpired() {
		delete(m.grants, normEmail)
		return nil, ErrGrantExpired
	}

	rec.Attempts++

	expectedHash := HashGrantCode(cleanCode, rec.Salt)
	if subtle.ConstantTimeCompare([]byte(rec.CodeHash), []byte(expectedHash)) != 1 {
		if rec.Attempts >= rec.MaxAttempts {
			delete(m.grants, normEmail)
			return nil, ErrGrantMaxAttempts
		}
		return nil, ErrInvalidGrantCode
	}

	// Code is valid! Mark burned.
	rec.Used = true
	now := time.Now().UTC()
	rec.UsedAt = &now

	recCopy := *rec
	return &recCopy, nil
}

// GetGrant retrieves the active grant record for an email (read-only inspect).
func (m *GrantManager) GetGrant(email string) (*GrantRecord, bool) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store != nil {
		rec, err := store.GetGrant(context.Background(), normEmail, "")
		if err != nil || rec == nil {
			return nil, false
		}
		return rec, true
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	rec, exists := m.grants[normEmail]
	if !exists {
		return nil, false
	}
	recCopy := *rec
	return &recCopy, true
}

// RevokeGrant deletes an active grant for the given email.
func (m *GrantManager) RevokeGrant(email string) bool {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	m.mu.Lock()
	defer m.mu.Unlock()

	deleted := false
	if _, exists := m.grants[normEmail]; exists {
		delete(m.grants, normEmail)
		deleted = true
	}

	if m.store != nil {
		rec, err := m.store.GetGrant(context.Background(), normEmail, "")
		if err == nil && rec != nil {
			_ = m.store.MarkUsed(context.Background(), rec.ID)
			deleted = true
		}
	}

	return deleted
}

// ListActiveGrants returns all active unexpired admin grants.
func (m *GrantManager) ListActiveGrants() ([]*GrantRecord, error) {
	if m.store != nil {
		return m.store.ListActiveGrants(context.Background())
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*GrantRecord
	for _, g := range m.grants {
		if !g.Used && !g.IsExpired() {
			cp := *g
			list = append(list, &cp)
		}
	}
	return list, nil
}

// Clear purges all grants from memory.
func (m *GrantManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.grants = make(map[string]*GrantRecord)
}
