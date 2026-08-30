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
	// DefaultChallengeTTL is the standard lifetime of an OTP challenge (10 minutes).
	DefaultChallengeTTL = 10 * time.Minute

	// DefaultMaxAttempts is the maximum number of failed verification attempts before invalidation.
	DefaultMaxAttempts = 3
)

// Challenge represents an ephemeral OTP verification challenge.
type Challenge struct {
	ID          string     `json:"id,omitempty"`
	Email       string     `json:"email"`
	CodeHash    string     `json:"code_hash"`
	Salt        string     `json:"salt"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	Verified    bool       `json:"verified,omitempty"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
}

// IsExpired returns true if the challenge has passed its expiration time.
func (c *Challenge) IsExpired() bool {
	if c == nil || c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().UTC().After(c.ExpiresAt)
}

// ChallengeStore defines the persistence interface required by ChallengeManager.
type ChallengeStore interface {
	SaveChallenge(ctx context.Context, ch *Challenge) error
	GetActiveChallenge(ctx context.Context, email string) (*Challenge, error)
	IncrementAttempts(ctx context.Context, id string) (int, error)
	MarkVerified(ctx context.Context, id string) error
	PruneExpired(ctx context.Context) error
}

// ChallengeManager manages active OTP verification challenges (in-memory or persistent).
type ChallengeManager struct {
	mu         sync.RWMutex
	challenges map[string]*Challenge
	store      ChallengeStore
}

// NewChallengeManager creates a new in-memory ChallengeManager instance.
func NewChallengeManager() *ChallengeManager {
	return &ChallengeManager{
		challenges: make(map[string]*Challenge),
	}
}

// NewPersistentChallengeManager creates a ChallengeManager backed by a persistent store.
func NewPersistentChallengeManager(store ChallengeStore) *ChallengeManager {
	return &ChallengeManager{
		challenges: make(map[string]*Challenge),
		store:      store,
	}
}

// WithStore configures a persistent ChallengeStore on the manager.
func (m *ChallengeManager) WithStore(store ChallengeStore) *ChallengeManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
	return m
}

// Store returns the configured ChallengeStore or nil if operating in-memory.
func (m *ChallengeManager) Store() ChallengeStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store
}

// GenerateOTP generates a cryptographically secure 6-digit numeric OTP string (100000-999999).
func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", fmt.Errorf("failed to generate secure OTP: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}

// HashOTP is an alias for HashCode.
func HashOTP(otp, salt string) string {
	return HashCode(otp, salt)
}

// HashCode computes a salted SHA-256 hash for a verification code.
func HashCode(code, salt string) string {
	h := sha256.Sum256([]byte(salt + ":" + code))
	return hex.EncodeToString(h[:])
}

// CreateChallenge generates a new 6-digit numeric OTP challenge for the given email with the specified TTL.
// If ttl <= 0, DefaultChallengeTTL (10 minutes) is used.
// It returns the Challenge record, the plaintext 6-digit code to be delivered, or an error.
func (m *ChallengeManager) CreateChallenge(email string, ttl time.Duration) (*Challenge, string, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return nil, "", errors.New("email cannot be empty")
	}

	if ttl <= 0 {
		ttl = DefaultChallengeTTL
	}

	code, err := GenerateOTP()
	if err != nil {
		return nil, "", err
	}

	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return nil, "", fmt.Errorf("failed to generate challenge salt: %w", err)
	}
	salt := hex.EncodeToString(saltBytes)

	codeHash := HashCode(code, salt)

	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return nil, "", fmt.Errorf("failed to generate challenge ID: %w", err)
	}
	chID := hex.EncodeToString(rawID)

	now := time.Now().UTC()
	ch := &Challenge{
		ID:          chID,
		Email:       normEmail,
		CodeHash:    codeHash,
		Salt:        salt,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
		Attempts:    0,
		MaxAttempts: DefaultMaxAttempts,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveChallenge(context.Background(), ch); err != nil {
			return nil, "", err
		}
	}
	m.challenges[normEmail] = ch

	return ch, code, nil
}

// CreateChallengeWithCode creates an active challenge for the given email with an explicit OTP code.
func (m *ChallengeManager) CreateChallengeWithCode(email, code string, ttl time.Duration) (*Challenge, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return nil, errors.New("email cannot be empty")
	}

	cleanCode := strings.TrimSpace(code)
	if cleanCode == "" {
		return nil, errors.New("code cannot be empty")
	}

	if ttl <= 0 {
		ttl = DefaultChallengeTTL
	}

	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return nil, fmt.Errorf("failed to generate challenge salt: %w", err)
	}
	salt := hex.EncodeToString(saltBytes)

	codeHash := HashCode(cleanCode, salt)

	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return nil, fmt.Errorf("failed to generate challenge ID: %w", err)
	}
	chID := hex.EncodeToString(rawID)

	now := time.Now().UTC()
	ch := &Challenge{
		ID:          chID,
		Email:       normEmail,
		CodeHash:    codeHash,
		Salt:        salt,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
		Attempts:    0,
		MaxAttempts: DefaultMaxAttempts,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveChallenge(context.Background(), ch); err != nil {
			return nil, err
		}
	}
	m.challenges[normEmail] = ch

	return ch, nil
}

// VerifyCode validates a submitted code against the active challenge for the email.
// If valid, the challenge is marked verified / consumed and returns true, nil.
// If invalid, attempts are incremented. If attempts >= MaxAttempts or expired, the challenge is invalidated.
func (m *ChallengeManager) VerifyCode(email, code string) (bool, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	cleanCode := strings.TrimSpace(code)

	if normEmail == "" {
		return false, errors.New("email cannot be empty")
	}
	if cleanCode == "" {
		return false, errors.New("code cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.store != nil {
		ch, err := m.store.GetActiveChallenge(context.Background(), normEmail)
		if err != nil {
			if errors.Is(err, ErrChallengeNotFound) {
				return false, ErrChallengeNotFound
			}
			return false, err
		}
		if ch == nil {
			return false, ErrChallengeNotFound
		}

		if ch.IsExpired() {
			_ = m.store.PruneExpired(context.Background())
			return false, ErrChallengeExpired
		}

		if ch.Attempts >= ch.MaxAttempts {
			return false, ErrMaxAttemptsExceeded
		}

		newAttempts, err := m.store.IncrementAttempts(context.Background(), ch.ID)
		if err != nil {
			return false, err
		}
		ch.Attempts = newAttempts

		expectedHash := HashCode(cleanCode, ch.Salt)
		if subtle.ConstantTimeCompare([]byte(ch.CodeHash), []byte(expectedHash)) != 1 {
			if ch.Attempts >= ch.MaxAttempts {
				return false, ErrMaxAttemptsExceeded
			}
			return false, ErrInvalidCode
		}

		if err := m.store.MarkVerified(context.Background(), ch.ID); err != nil {
			return false, err
		}
		delete(m.challenges, normEmail)
		return true, nil
	}

	ch, exists := m.challenges[normEmail]
	if !exists {
		return false, ErrChallengeNotFound
	}

	if ch.IsExpired() {
		delete(m.challenges, normEmail)
		return false, ErrChallengeExpired
	}

	ch.Attempts++

	expectedHash := HashCode(cleanCode, ch.Salt)
	if subtle.ConstantTimeCompare([]byte(ch.CodeHash), []byte(expectedHash)) != 1 {
		if ch.Attempts >= ch.MaxAttempts {
			delete(m.challenges, normEmail)
			return false, ErrMaxAttemptsExceeded
		}
		return false, ErrInvalidCode
	}

	// Code is valid! Consume and remove the challenge.
	delete(m.challenges, normEmail)
	return true, nil
}

// GetChallenge retrieves an active challenge for the given email (read-only inspect).
func (m *ChallengeManager) GetChallenge(email string) (*Challenge, bool) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store != nil {
		ch, err := store.GetActiveChallenge(context.Background(), normEmail)
		if err != nil || ch == nil {
			return nil, false
		}
		return ch, true
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	ch, exists := m.challenges[normEmail]
	if !exists {
		return nil, false
	}
	chCopy := *ch
	return &chCopy, true
}

// Clear removes all active challenges.
func (m *ChallengeManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		_ = m.store.PruneExpired(context.Background())
	}
	m.challenges = make(map[string]*Challenge)
}
