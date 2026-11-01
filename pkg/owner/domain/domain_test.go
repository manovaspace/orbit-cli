package domain_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner/domain"
)

func TestGenerateOTP(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		otp, err := domain.GenerateOTP()
		if err != nil {
			t.Fatalf("GenerateOTP failed: %v", err)
		}

		if len(otp) != 6 {
			t.Fatalf("expected 6-digit OTP, got %q (len %d)", otp, len(otp))
		}

		num, err := strconv.Atoi(otp)
		if err != nil {
			t.Fatalf("expected numeric OTP, got %q: %v", otp, err)
		}

		if num < 100000 || num > 999999 {
			t.Fatalf("expected OTP between 100000 and 999999, got %d", num)
		}

		seen[otp] = true
	}

	if len(seen) < 40 {
		t.Fatalf("expected diverse OTPs across 50 iterations, got only %d unique", len(seen))
	}
}

func TestCreateChallenge(t *testing.T) {
	mgr := domain.NewChallengeManager()

	email := "Admin@Example.Com"
	ch, code, err := mgr.CreateChallenge(email, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	if ch.Email != "admin@example.com" {
		t.Errorf("expected normalized email, got %q", ch.Email)
	}

	if len(code) != 6 {
		t.Errorf("expected 6-digit code, got %q", code)
	}

	if ch.Attempts != 0 {
		t.Errorf("expected 0 initial attempts, got %d", ch.Attempts)
	}

	if ch.MaxAttempts != 3 {
		t.Errorf("expected max attempts 3, got %d", ch.MaxAttempts)
	}

	expectedHash := domain.HashCode(code, ch.Salt)
	if ch.CodeHash != expectedHash {
		t.Errorf("expected code hash %s, got %s", expectedHash, ch.CodeHash)
	}

	// Empty email error
	_, _, err = mgr.CreateChallenge("", 10*time.Minute)
	if err == nil {
		t.Errorf("expected error for empty email, got nil")
	}
}

func TestVerifyCode_Success(t *testing.T) {
	mgr := domain.NewChallengeManager()

	email := "test@example.com"
	_, code, err := mgr.CreateChallenge(email, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	ok, err := mgr.VerifyCode(email, code)
	if err != nil {
		t.Fatalf("VerifyCode failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected VerifyCode to return true")
	}

	// Re-verifying should fail because challenge was consumed
	ok, err = mgr.VerifyCode(email, code)
	if err != domain.ErrChallengeNotFound {
		t.Fatalf("expected ErrChallengeNotFound on replay, got: %v", err)
	}
	if ok {
		t.Fatalf("expected ok to be false on replay")
	}
}

func TestVerifyCode_InvalidCode(t *testing.T) {
	mgr := domain.NewChallengeManager()

	email := "test@example.com"
	_, _, err := mgr.CreateChallenge(email, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	ok, err := mgr.VerifyCode(email, "000000")
	if err != domain.ErrInvalidCode {
		t.Fatalf("expected ErrInvalidCode, got: %v", err)
	}
	if ok {
		t.Fatalf("expected ok to be false")
	}

	ch, exists := mgr.GetChallenge(email)
	if !exists {
		t.Fatalf("expected challenge to still exist after 1 failed attempt")
	}
	if ch.Attempts != 1 {
		t.Errorf("expected 1 attempt recorded, got %d", ch.Attempts)
	}
}

func TestVerifyCode_MaxAttempts(t *testing.T) {
	mgr := domain.NewChallengeManager()

	email := "test@example.com"
	_, _, err := mgr.CreateChallenge(email, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	// Attempt 1
	ok, err := mgr.VerifyCode(email, "111111")
	if err != domain.ErrInvalidCode || ok {
		t.Fatalf("attempt 1: expected ErrInvalidCode, got %v (ok: %v)", err, ok)
	}

	// Attempt 2
	ok, err = mgr.VerifyCode(email, "222222")
	if err != domain.ErrInvalidCode || ok {
		t.Fatalf("attempt 2: expected ErrInvalidCode, got %v (ok: %v)", err, ok)
	}

	// Attempt 3 (Max limit reached -> invalidated)
	ok, err = mgr.VerifyCode(email, "333333")
	if err != domain.ErrMaxAttemptsExceeded || ok {
		t.Fatalf("attempt 3: expected ErrMaxAttemptsExceeded, got %v (ok: %v)", err, ok)
	}

	// Challenge should be gone
	_, exists := mgr.GetChallenge(email)
	if exists {
		t.Fatalf("expected challenge to be removed after exceeding max attempts")
	}
}

func TestVerifyCode_Expired(t *testing.T) {
	mgr := domain.NewChallengeManager()

	email := "test@example.com"
	_, code, err := mgr.CreateChallenge(email, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	time.Sleep(25 * time.Millisecond)

	ok, err := mgr.VerifyCode(email, code)
	if err != domain.ErrChallengeExpired {
		t.Fatalf("expected ErrChallengeExpired, got: %v", err)
	}
	if ok {
		t.Fatalf("expected ok to be false")
	}
}

func TestGenerateMasterSecret(t *testing.T) {
	secret1, err := domain.GenerateMasterSecret()
	if err != nil {
		t.Fatalf("GenerateMasterSecret failed: %v", err)
	}
	if len(secret1) != 64 {
		t.Fatalf("expected 64 hex characters (32 bytes), got length %d", len(secret1))
	}

	secret2, err := domain.GenerateMasterSecret()
	if err != nil {
		t.Fatalf("GenerateMasterSecret failed: %v", err)
	}
	if secret1 == secret2 {
		t.Fatalf("expected unique secrets, got identical")
	}
}

func TestCreateChallengeWithCode(t *testing.T) {
	mgr := domain.NewChallengeManager()
	email := "owner@example.com"
	code := "654321"

	ch, err := mgr.CreateChallengeWithCode(email, code, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallengeWithCode failed: %v", err)
	}

	if ch.Email != email {
		t.Errorf("expected email %s, got %s", email, ch.Email)
	}

	ok, err := mgr.VerifyCode(email, code)
	if err != nil || !ok {
		t.Fatalf("expected VerifyCode to succeed with explicit code, got ok=%v, err=%v", ok, err)
	}
}

type mockChallengeStore struct {
	challenges map[string]*domain.Challenge
}

func newMockChallengeStore() *mockChallengeStore {
	return &mockChallengeStore{challenges: make(map[string]*domain.Challenge)}
}

func (m *mockChallengeStore) SaveChallenge(ctx context.Context, ch *domain.Challenge) error {
	m.challenges[ch.Email] = ch
	return nil
}

func (m *mockChallengeStore) GetActiveChallenge(ctx context.Context, email string) (*domain.Challenge, error) {
	ch, ok := m.challenges[email]
	if !ok || ch.Verified || ch.IsExpired() {
		return nil, domain.ErrChallengeNotFound
	}
	return ch, nil
}

func (m *mockChallengeStore) IncrementAttempts(ctx context.Context, id string) (int, error) {
	for _, ch := range m.challenges {
		if ch.ID == id {
			ch.Attempts++
			return ch.Attempts, nil
		}
	}
	return 0, domain.ErrChallengeNotFound
}

func (m *mockChallengeStore) MarkVerified(ctx context.Context, id string) error {
	for _, ch := range m.challenges {
		if ch.ID == id {
			ch.Verified = true
			now := time.Now().UTC()
			ch.VerifiedAt = &now
			return nil
		}
	}
	return domain.ErrChallengeNotFound
}

func (m *mockChallengeStore) PruneExpired(ctx context.Context) error {
	for email, ch := range m.challenges {
		if !ch.Verified && ch.IsExpired() {
			delete(m.challenges, email)
		}
	}
	return nil
}

func TestPersistentChallengeManager(t *testing.T) {
	mockStore := newMockChallengeStore()
	mgr := domain.NewPersistentChallengeManager(mockStore)

	if mgr.Store() != domain.ChallengeStore(mockStore) {
		t.Errorf("expected Store() to return configured mockStore")
	}

	email := "persist-challenge@manova.space"
	ch, code, err := mgr.CreateChallenge(email, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	if _, ok := mockStore.challenges[email]; !ok {
		t.Fatalf("expected challenge to be stored in mockStore")
	}

	inspected, ok := mgr.GetChallenge(email)
	if !ok || inspected.ID != ch.ID {
		t.Fatalf("GetChallenge failed or ID mismatch")
	}

	// Invalid verification
	valid, err := mgr.VerifyCode(email, "000000")
	if valid || err != domain.ErrInvalidCode {
		t.Fatalf("expected ErrInvalidCode, got: %v (valid: %v)", err, valid)
	}

	// Successful verification
	valid, err = mgr.VerifyCode(email, code)
	if !valid || err != nil {
		t.Fatalf("expected VerifyCode success, got: %v (valid: %v)", err, valid)
	}

	// Replay should fail
	valid, err = mgr.VerifyCode(email, code)
	if valid || err != domain.ErrChallengeNotFound {
		t.Fatalf("expected ErrChallengeNotFound on replay, got: %v", err)
	}
}
