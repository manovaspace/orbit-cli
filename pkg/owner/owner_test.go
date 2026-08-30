package owner_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner"
)

func TestGenerateOTP(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		otp, err := owner.GenerateOTP()
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
	mgr := owner.NewChallengeManager()

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

	expectedHash := owner.HashCode(code, ch.Salt)
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
	mgr := owner.NewChallengeManager()

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
	if err != owner.ErrChallengeNotFound {
		t.Fatalf("expected ErrChallengeNotFound on replay, got: %v", err)
	}
	if ok {
		t.Fatalf("expected ok to be false on replay")
	}
}

func TestVerifyCode_InvalidCode(t *testing.T) {
	mgr := owner.NewChallengeManager()

	email := "test@example.com"
	_, _, err := mgr.CreateChallenge(email, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	ok, err := mgr.VerifyCode(email, "000000")
	if err != owner.ErrInvalidCode {
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
	mgr := owner.NewChallengeManager()

	email := "test@example.com"
	_, _, err := mgr.CreateChallenge(email, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	// Attempt 1
	ok, err := mgr.VerifyCode(email, "111111")
	if err != owner.ErrInvalidCode || ok {
		t.Fatalf("attempt 1: expected ErrInvalidCode, got %v (ok: %v)", err, ok)
	}

	// Attempt 2
	ok, err = mgr.VerifyCode(email, "222222")
	if err != owner.ErrInvalidCode || ok {
		t.Fatalf("attempt 2: expected ErrInvalidCode, got %v (ok: %v)", err, ok)
	}

	// Attempt 3 (Max limit reached -> invalidated)
	ok, err = mgr.VerifyCode(email, "333333")
	if err != owner.ErrMaxAttemptsExceeded || ok {
		t.Fatalf("attempt 3: expected ErrMaxAttemptsExceeded, got %v (ok: %v)", err, ok)
	}

	// Challenge should be gone
	_, exists := mgr.GetChallenge(email)
	if exists {
		t.Fatalf("expected challenge to be removed after exceeding max attempts")
	}
}

func TestVerifyCode_Expired(t *testing.T) {
	mgr := owner.NewChallengeManager()

	email := "test@example.com"
	_, code, err := mgr.CreateChallenge(email, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("CreateChallenge failed: %v", err)
	}

	time.Sleep(25 * time.Millisecond)

	ok, err := mgr.VerifyCode(email, code)
	if err != owner.ErrChallengeExpired {
		t.Fatalf("expected ErrChallengeExpired, got: %v", err)
	}
	if ok {
		t.Fatalf("expected ok to be false")
	}
}

func TestGenerateMasterSecret(t *testing.T) {
	secret1, err := owner.GenerateMasterSecret()
	if err != nil {
		t.Fatalf("GenerateMasterSecret failed: %v", err)
	}
	if len(secret1) != 64 {
		t.Fatalf("expected 64 hex characters (32 bytes), got length %d", len(secret1))
	}

	secret2, err := owner.GenerateMasterSecret()
	if err != nil {
		t.Fatalf("GenerateMasterSecret failed: %v", err)
	}
	if secret1 == secret2 {
		t.Fatalf("expected unique secrets, got identical")
	}
}

func TestStore_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "nested", "orbit", "owner.json")

	s := owner.NewStore(storePath)

	if s.IsVerified() {
		t.Fatalf("expected unverified before save")
	}

	_, err := s.LoadOwner()
	if err != owner.ErrOwnerNotFound {
		t.Fatalf("expected ErrOwnerNotFound for missing store, got: %v", err)
	}

	secret, err := owner.GenerateMasterSecret()
	if err != nil {
		t.Fatalf("GenerateMasterSecret failed: %v", err)
	}

	rec := &owner.OwnerRecord{
		Email:             "admin@example.com",
		DisplayName:       "Alireza",
		VerifiedAt:        time.Now().UTC().Truncate(time.Second),
		RootSigningSecret: secret,
	}

	if err := s.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	// Verify file mode is 0600
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected file mode 0600, got %o", perm)
	}

	// Load record back
	loaded, err := s.LoadOwner()
	if err != nil {
		t.Fatalf("LoadOwner failed: %v", err)
	}

	if loaded.Email != rec.Email {
		t.Errorf("expected email %s, got %s", rec.Email, loaded.Email)
	}
	if loaded.DisplayName != rec.DisplayName {
		t.Errorf("expected display name %s, got %s", rec.DisplayName, loaded.DisplayName)
	}
	if loaded.RootSigningSecret != rec.RootSigningSecret {
		t.Errorf("expected secret %s, got %s", rec.RootSigningSecret, loaded.RootSigningSecret)
	}
	if loaded.KeyFingerprint == "" {
		t.Errorf("expected non-empty key fingerprint")
	}
	if !loaded.VerifiedAt.Equal(rec.VerifiedAt) {
		t.Errorf("expected verified_at %v, got %v", rec.VerifiedAt, loaded.VerifiedAt)
	}

	if !s.IsVerified() {
		t.Fatalf("expected IsVerified() to be true")
	}
}

func TestStore_DefaultPath(t *testing.T) {
	t.Run("ORBIT_OWNER_STORE env override", func(t *testing.T) {
		customPath := "/tmp/custom-owner.json"
		t.Setenv("ORBIT_OWNER_STORE", customPath)
		p := owner.DefaultStorePath()
		if p != customPath {
			t.Fatalf("expected %s, got %s", customPath, p)
		}
	})

	t.Run("Default home config path", func(t *testing.T) {
		t.Setenv("ORBIT_OWNER_STORE", "")
		p := owner.DefaultStorePath()
		if !strings.HasSuffix(p, filepath.Join(".config", "orbit", "owner.json")) && p != "/etc/orbit/owner.json" {
			t.Fatalf("unexpected default store path: %s", p)
		}
	})
}

func TestStore_InsecurePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "owner.json")
	s := owner.NewStore(storePath)

	rec := &owner.OwnerRecord{
		Email:             "test@example.com",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: "secret123",
	}

	if err := s.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	if err := s.CheckPermissions(); err != nil {
		t.Fatalf("expected secure permissions check, got: %v", err)
	}

	// Make permissions insecure
	if err := os.Chmod(storePath, 0644); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	if err := s.CheckPermissions(); err != owner.ErrInsecurePermissions {
		t.Fatalf("expected ErrInsecurePermissions, got: %v", err)
	}
}

func TestCreateChallengeWithCode(t *testing.T) {
	mgr := owner.NewChallengeManager()
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

