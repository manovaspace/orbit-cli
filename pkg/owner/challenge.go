package owner

import (
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

// ChallengeManager manages active in-memory OTP verification challenges.
type ChallengeManager struct {
	mu         sync.RWMutex
	challenges map[string]*Challenge
}

// NewChallengeManager creates a new ChallengeManager instance.
func NewChallengeManager() *ChallengeManager {
	return &ChallengeManager{
		challenges: make(map[string]*Challenge),
	}
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

	ch := &Challenge{
		Email:       normEmail,
		CodeHash:    codeHash,
		Salt:        salt,
		ExpiresAt:   time.Now().UTC().Add(ttl),
		Attempts:    0,
		MaxAttempts: DefaultMaxAttempts,
	}

	m.mu.Lock()
	m.challenges[normEmail] = ch
	m.mu.Unlock()

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

	ch := &Challenge{
		Email:       normEmail,
		CodeHash:    codeHash,
		Salt:        salt,
		ExpiresAt:   time.Now().UTC().Add(ttl),
		Attempts:    0,
		MaxAttempts: DefaultMaxAttempts,
	}

	m.mu.Lock()
	m.challenges[normEmail] = ch
	m.mu.Unlock()

	return ch, nil
}

// VerifyCode validates a submitted code against the active challenge for the email.
// If valid, the challenge is removed and returns true, nil.
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
	m.challenges = make(map[string]*Challenge)
}
