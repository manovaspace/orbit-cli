package owner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// mockStore implements ChallengeStore for testing persistent challenge operations.
type mockStore struct {
	mu         sync.Mutex
	challenges map[string]*Challenge
}

func newMockStore() *mockStore {
	return &mockStore{
		challenges: make(map[string]*Challenge),
	}
}

func (m *mockStore) SaveChallenge(ctx context.Context, ch *Challenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	chCopy := *ch
	m.challenges[ch.Email] = &chCopy
	return nil
}

func (m *mockStore) GetActiveChallenge(ctx context.Context, email string) (*Challenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.challenges[email]
	if !ok || ch.Verified {
		return nil, ErrChallengeNotFound
	}
	chCopy := *ch
	return &chCopy, nil
}

func (m *mockStore) IncrementAttempts(ctx context.Context, id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.challenges {
		if ch.ID == id {
			ch.Attempts++
			return ch.Attempts, nil
		}
	}
	return 0, ErrChallengeNotFound
}

func (m *mockStore) MarkVerified(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.challenges {
		if ch.ID == id {
			ch.Verified = true
			now := time.Now().UTC()
			ch.VerifiedAt = &now
			return nil
		}
	}
	return ErrChallengeNotFound
}

func (m *mockStore) PruneExpired(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for email, ch := range m.challenges {
		if ch.IsExpired() {
			delete(m.challenges, email)
		}
	}
	return nil
}

func TestChallengeManager_CreateAndVerify(t *testing.T) {
	t.Run("In-memory Create and Verify Success", func(t *testing.T) {
		mgr := NewChallengeManager()
		email := "  Alice@Example.COM  "
		ttl := 5 * time.Minute

		ch, code, err := mgr.CreateChallenge(email, ttl)
		if err != nil {
			t.Fatalf("CreateChallenge failed: %v", err)
		}

		if ch == nil {
			t.Fatal("expected non-nil challenge")
		}

		// Verify 6-digit numeric OTP format
		if len(code) != 6 {
			t.Fatalf("expected 6-digit code, got %q (len %d)", code, len(code))
		}
		if _, err := strconv.Atoi(code); err != nil {
			t.Fatalf("expected numeric OTP, got %q: %v", code, err)
		}

		// Verify normalized email and initial challenge properties
		expectedEmail := "alice@example.com"
		if ch.Email != expectedEmail {
			t.Errorf("expected email %q, got %q", expectedEmail, ch.Email)
		}
		if ch.Attempts != 0 {
			t.Errorf("expected initial attempts 0, got %d", ch.Attempts)
		}
		if ch.MaxAttempts != DefaultMaxAttempts {
			t.Errorf("expected MaxAttempts %d, got %d", DefaultMaxAttempts, ch.MaxAttempts)
		}
		if ch.ID == "" {
			t.Error("expected non-empty challenge ID")
		}
		if len(ch.Salt) != 32 { // 16 bytes = 32 hex chars
			t.Errorf("expected 32-char hex salt, got length %d", len(ch.Salt))
		}

		// Verify cryptographic hash matches HashCode
		expectedHash := HashCode(code, ch.Salt)
		if ch.CodeHash != expectedHash {
			t.Errorf("expected code_hash %q, got %q", expectedHash, ch.CodeHash)
		}

		// Verify HashOTP is identical to HashCode
		if HashOTP(code, ch.Salt) != expectedHash {
			t.Error("expected HashOTP to match HashCode")
		}

		// Inspect via GetChallenge
		inspected, exists := mgr.GetChallenge(expectedEmail)
		if !exists || inspected == nil {
			t.Fatal("expected challenge to exist in manager")
		}
		if inspected.ID != ch.ID {
			t.Errorf("expected ID %q, got %q", ch.ID, inspected.ID)
		}

		// 1. Incorrect code should fail and increment attempts
		badCode := "000000"
		if badCode == code {
			badCode = "999999"
		}
		ok, err := mgr.VerifyCode(expectedEmail, badCode)
		if ok || !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("expected ErrInvalidCode on wrong code, got ok=%v, err=%v", ok, err)
		}

		chAfterBad, exists := mgr.GetChallenge(expectedEmail)
		if !exists {
			t.Fatal("expected challenge to still exist after 1 failed attempt")
		}
		if chAfterBad.Attempts != 1 {
			t.Errorf("expected attempts 1, got %d", chAfterBad.Attempts)
		}

		// 2. Correct code verification succeeds
		ok, err = mgr.VerifyCode("ALICE@example.com", code)
		if !ok || err != nil {
			t.Fatalf("expected VerifyCode to succeed, got ok=%v, err=%v", ok, err)
		}

		// 3. Challenge is consumed - subsequent attempts fail with ErrChallengeNotFound
		ok, err = mgr.VerifyCode(expectedEmail, code)
		if ok || !errors.Is(err, ErrChallengeNotFound) {
			t.Fatalf("expected ErrChallengeNotFound after consumption, got ok=%v, err=%v", ok, err)
		}
		_, exists = mgr.GetChallenge(expectedEmail)
		if exists {
			t.Error("expected challenge to be removed after successful verification")
		}
	})

	t.Run("Persistent Store Create and Verify", func(t *testing.T) {
		store := newMockStore()
		mgr := NewPersistentChallengeManager(store)

		if mgr.Store() != store {
			t.Error("expected Store() to return configured store")
		}

		email := "bob@example.com"
		ch, code, err := mgr.CreateChallenge(email, 10*time.Minute)
		if err != nil {
			t.Fatalf("CreateChallenge failed: %v", err)
		}

		// Inspect via manager
		inspected, exists := mgr.GetChallenge(email)
		if !exists || inspected.ID != ch.ID {
			t.Fatal("expected challenge to exist in persistent manager")
		}

		// Bad code fails and increments attempts
		badCode := "123456"
		if badCode == code {
			badCode = "654321"
		}
		ok, err := mgr.VerifyCode(email, badCode)
		if ok || !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("expected ErrInvalidCode, got ok=%v, err=%v", ok, err)
		}

		// Correct code succeeds
		ok, err = mgr.VerifyCode(email, code)
		if !ok || err != nil {
			t.Fatalf("expected VerifyCode to succeed, got ok=%v, err=%v", ok, err)
		}

		// Challenge marked verified in store
		store.mu.Lock()
		storedCh := store.challenges[email]
		store.mu.Unlock()
		if storedCh == nil || !storedCh.Verified || storedCh.VerifiedAt == nil {
			t.Error("expected challenge to be marked verified in persistent store")
		}
	})

	t.Run("CreateChallengeWithCode Explicit OTP", func(t *testing.T) {
		mgr := NewChallengeManager()
		email := "carol@example.com"
		explicitCode := "837492"

		ch, err := mgr.CreateChallengeWithCode(email, explicitCode, 0) // default TTL
		if err != nil {
			t.Fatalf("CreateChallengeWithCode failed: %v", err)
		}
		if ch.Email != email {
			t.Errorf("expected email %q, got %q", email, ch.Email)
		}

		ok, err := mgr.VerifyCode(email, explicitCode)
		if !ok || err != nil {
			t.Fatalf("expected VerifyCode to succeed with explicit code, got ok=%v, err=%v", ok, err)
		}
	})

	t.Run("Input Validation Edge Cases", func(t *testing.T) {
		mgr := NewChallengeManager()

		// Empty email
		_, _, err := mgr.CreateChallenge("", time.Minute)
		if err == nil {
			t.Error("expected error for empty email in CreateChallenge")
		}

		_, err = mgr.CreateChallengeWithCode("", "123456", time.Minute)
		if err == nil {
			t.Error("expected error for empty email in CreateChallengeWithCode")
		}

		_, err = mgr.CreateChallengeWithCode("test@example.com", "", time.Minute)
		if err == nil {
			t.Error("expected error for empty code in CreateChallengeWithCode")
		}

		_, err = mgr.VerifyCode("", "123456")
		if err == nil {
			t.Error("expected error for empty email in VerifyCode")
		}

		_, err = mgr.VerifyCode("test@example.com", "")
		if err == nil {
			t.Error("expected error for empty code in VerifyCode")
		}

		// Non-existent email verification
		ok, err := mgr.VerifyCode("unknown@example.com", "123456")
		if ok || !errors.Is(err, ErrChallengeNotFound) {
			t.Fatalf("expected ErrChallengeNotFound for non-existent challenge, got ok=%v, err=%v", ok, err)
		}
	})

	t.Run("WithStore Configuration", func(t *testing.T) {
		mgr := NewChallengeManager()
		if mgr.Store() != nil {
			t.Error("expected nil store initially")
		}
		store := newMockStore()
		mgr.WithStore(store)
		if mgr.Store() != store {
			t.Error("expected store to be set via WithStore")
		}
	})

	t.Run("Cryptographic Hash Verification", func(t *testing.T) {
		salt := "a1b2c3d4e5f60718293a4b5c6d7e8f90"
		code := "654321"

		// Manual calculation of SHA-256(salt + ":" + code)
		expectedRaw := sha256.Sum256([]byte(salt + ":" + code))
		expectedHex := hex.EncodeToString(expectedRaw[:])

		computed := HashCode(code, salt)
		if computed != expectedHex {
			t.Fatalf("HashCode mismatch: expected %s, got %s", expectedHex, computed)
		}
	})
}

func TestChallengeManager_MaxAttemptsLockout(t *testing.T) {
	t.Run("In-Memory Max Attempts Lockout", func(t *testing.T) {
		mgr := NewChallengeManager()
		email := "lockout-test@example.com"

		_, correctCode, err := mgr.CreateChallenge(email, 10*time.Minute)
		if err != nil {
			t.Fatalf("CreateChallenge failed: %v", err)
		}

		// Attempt 1: Failed attempt
		ok, err := mgr.VerifyCode(email, "000001")
		if ok || !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("attempt 1: expected ErrInvalidCode, got ok=%v, err=%v", ok, err)
		}

		// Attempt 2: Failed attempt
		ok, err = mgr.VerifyCode(email, "000002")
		if ok || !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("attempt 2: expected ErrInvalidCode, got ok=%v, err=%v", ok, err)
		}

		// Attempt 3: Failed attempt reaches DefaultMaxAttempts (3) -> Lockout
		ok, err = mgr.VerifyCode(email, "000003")
		if ok || !errors.Is(err, ErrMaxAttemptsExceeded) {
			t.Fatalf("attempt 3: expected ErrMaxAttemptsExceeded, got ok=%v, err=%v", ok, err)
		}

		// Challenge should be purged from memory after lockout
		_, exists := mgr.GetChallenge(email)
		if exists {
			t.Error("expected challenge to be purged from memory after max attempts reached")
		}

		// Attempt 4: Even providing the CORRECT code fails due to lockout / missing challenge
		ok, err = mgr.VerifyCode(email, correctCode)
		if ok || !errors.Is(err, ErrChallengeNotFound) {
			t.Fatalf("expected ErrChallengeNotFound on correct code after lockout, got ok=%v, err=%v", ok, err)
		}
	})

	t.Run("Persistent Store Max Attempts Lockout", func(t *testing.T) {
		store := newMockStore()
		mgr := NewPersistentChallengeManager(store)
		email := "persist-lockout@example.com"

		_, correctCode, err := mgr.CreateChallenge(email, 10*time.Minute)
		if err != nil {
			t.Fatalf("CreateChallenge failed: %v", err)
		}

		// Fail 2 attempts
		for i := 1; i <= 2; i++ {
			ok, err := mgr.VerifyCode(email, "000000")
			if ok || !errors.Is(err, ErrInvalidCode) {
				t.Fatalf("attempt %d: expected ErrInvalidCode, got ok=%v, err=%v", i, ok, err)
			}
		}

		// Fail 3rd attempt -> Max attempts exceeded
		ok, err := mgr.VerifyCode(email, "000000")
		if ok || !errors.Is(err, ErrMaxAttemptsExceeded) {
			t.Fatalf("attempt 3: expected ErrMaxAttemptsExceeded, got ok=%v, err=%v", ok, err)
		}

		// Attempt with correct code now fails with ErrMaxAttemptsExceeded
		ok, err = mgr.VerifyCode(email, correctCode)
		if ok || !errors.Is(err, ErrMaxAttemptsExceeded) {
			t.Fatalf("expected ErrMaxAttemptsExceeded on correct code after persistent lockout, got ok=%v, err=%v", ok, err)
		}
	})
}

func TestChallengeManager_Expiration(t *testing.T) {
	t.Run("Challenge IsExpired Helper", func(t *testing.T) {
		// Nil challenge
		var nilCh *Challenge
		if nilCh.IsExpired() {
			t.Error("expected nil challenge IsExpired to return false")
		}

		// Zero expiration time
		zeroCh := &Challenge{}
		if zeroCh.IsExpired() {
			t.Error("expected zero ExpiresAt challenge IsExpired to return false")
		}

		// Future expiration time
		futureCh := &Challenge{
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		}
		if futureCh.IsExpired() {
			t.Error("expected future challenge IsExpired to return false")
		}

		// Past expiration time
		pastCh := &Challenge{
			ExpiresAt: time.Now().UTC().Add(-10 * time.Minute),
		}
		if !pastCh.IsExpired() {
			t.Error("expected past challenge IsExpired to return true")
		}
	})

	t.Run("In-Memory Expiration Rejects Verification", func(t *testing.T) {
		mgr := NewChallengeManager()
		email := "expire-mem@example.com"
		shortTTL := 15 * time.Millisecond

		_, code, err := mgr.CreateChallenge(email, shortTTL)
		if err != nil {
			t.Fatalf("CreateChallenge failed: %v", err)
		}

		time.Sleep(30 * time.Millisecond)

		// Verification with valid code must be rejected due to expiration
		ok, err := mgr.VerifyCode(email, code)
		if ok || !errors.Is(err, ErrChallengeExpired) {
			t.Fatalf("expected ErrChallengeExpired, got ok=%v, err=%v", ok, err)
		}

		// Challenge should be purged from memory on expired access
		_, exists := mgr.GetChallenge(email)
		if exists {
			t.Error("expected expired challenge to be purged from memory")
		}
	})

	t.Run("Persistent Store Expiration and PruneExpired", func(t *testing.T) {
		store := newMockStore()
		mgr := NewPersistentChallengeManager(store)
		email := "expire-persist@example.com"
		shortTTL := 15 * time.Millisecond

		_, code, err := mgr.CreateChallenge(email, shortTTL)
		if err != nil {
			t.Fatalf("CreateChallenge failed: %v", err)
		}

		time.Sleep(30 * time.Millisecond)

		// Verification triggers PruneExpired and returns ErrChallengeExpired
		ok, err := mgr.VerifyCode(email, code)
		if ok || !errors.Is(err, ErrChallengeExpired) {
			t.Fatalf("expected ErrChallengeExpired, got ok=%v, err=%v", ok, err)
		}

		// Store should have pruned the expired challenge
		store.mu.Lock()
		_, inStore := store.challenges[email]
		store.mu.Unlock()
		if inStore {
			t.Error("expected expired challenge to be pruned from persistent store")
		}
	})

	t.Run("PruneExpired Directly on Store", func(t *testing.T) {
		store := newMockStore()
		ctx := context.Background()

		// Save active challenge
		activeCh := &Challenge{
			ID:        "active-1",
			Email:     "active@example.com",
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		}
		if err := store.SaveChallenge(ctx, activeCh); err != nil {
			t.Fatalf("SaveChallenge failed: %v", err)
		}

		// Save expired challenge
		expiredCh := &Challenge{
			ID:        "expired-1",
			Email:     "expired@example.com",
			ExpiresAt: time.Now().UTC().Add(-5 * time.Minute),
		}
		if err := store.SaveChallenge(ctx, expiredCh); err != nil {
			t.Fatalf("SaveChallenge failed: %v", err)
		}

		// Run PruneExpired
		if err := store.PruneExpired(ctx); err != nil {
			t.Fatalf("PruneExpired failed: %v", err)
		}

		store.mu.Lock()
		defer store.mu.Unlock()
		if _, ok := store.challenges["active@example.com"]; !ok {
			t.Error("expected active challenge to remain after prune")
		}
		if _, ok := store.challenges["expired@example.com"]; ok {
			t.Error("expected expired challenge to be pruned")
		}
	})

	t.Run("Clear Method Cleans All In-Memory Challenges", func(t *testing.T) {
		mgr := NewChallengeManager()

		_, _, _ = mgr.CreateChallenge("u1@example.com", 10*time.Minute)
		_, _, _ = mgr.CreateChallenge("u2@example.com", 10*time.Minute)

		mgr.Clear()

		if _, exists := mgr.GetChallenge("u1@example.com"); exists {
			t.Error("expected u1 challenge to be cleared")
		}
		if _, exists := mgr.GetChallenge("u2@example.com"); exists {
			t.Error("expected u2 challenge to be cleared")
		}
	})

	t.Run("Clear Method Prunes Expired on Persistent Manager", func(t *testing.T) {
		store := newMockStore()
		mgr := NewPersistentChallengeManager(store)

		_, _, _ = mgr.CreateChallenge("u1@example.com", 10*time.Millisecond)
		time.Sleep(25 * time.Millisecond)

		mgr.Clear()

		if _, exists := mgr.GetChallenge("u1@example.com"); exists {
			t.Error("expected expired challenge to be pruned on Clear")
		}
	})
}
