package owner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerate8DigitCode(t *testing.T) {
	code, err := Generate8DigitCode()
	if err != nil {
		t.Fatalf("Generate8DigitCode failed: %v", err)
	}

	if len(code) != 9 || !strings.Contains(code, "-") {
		t.Fatalf("expected formatted code XXXX-XXXX, got %s", code)
	}

	clean := CleanCode(code)
	if len(clean) != 8 {
		t.Fatalf("expected 8 digits, got %s", clean)
	}
}

func TestGrantManagerLifecycle(t *testing.T) {
	gm := NewGrantManager()

	email := "sara@manova.space"
	rec, code, err := gm.CreateGrant(email, "admin", "admin@manova.space", 15*time.Minute)
	if err != nil {
		t.Fatalf("CreateGrant failed: %v", err)
	}

	if rec.Email != email {
		t.Errorf("expected email %s, got %s", email, rec.Email)
	}
	if rec.Role != "admin" {
		t.Errorf("expected role admin, got %s", rec.Role)
	}
	if rec.Used {
		t.Errorf("expected new grant to be unused")
	}

	// 1. Inspect
	inspected, ok := gm.GetGrant(email)
	if !ok || inspected.ID != rec.ID {
		t.Fatalf("GetGrant failed")
	}

	// 2. Invalid code verification
	_, err = gm.VerifyGrant(email, "0000-0000")
	if !errors.Is(err, ErrInvalidGrantCode) {
		t.Fatalf("expected ErrInvalidGrantCode, got %v", err)
	}

	// 3. Successful verification with formatted code
	verified, err := gm.VerifyGrant(email, code)
	if err != nil {
		t.Fatalf("VerifyGrant failed: %v", err)
	}
	if !verified.Used || verified.UsedAt == nil {
		t.Errorf("expected grant to be marked used")
	}

	// 4. Replay attempt should fail with ErrGrantAlreadyUsed
	_, err = gm.VerifyGrant(email, code)
	if !errors.Is(err, ErrGrantAlreadyUsed) {
		t.Fatalf("expected ErrGrantAlreadyUsed on replay, got %v", err)
	}
}

func TestGrantManagerMaxAttempts(t *testing.T) {
	gm := NewGrantManager()
	email := "attacker@test.local"

	_, _, err := gm.CreateGrant(email, "admin", "admin@manova.space", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt 1
	_, err = gm.VerifyGrant(email, "1111-1111")
	if !errors.Is(err, ErrInvalidGrantCode) {
		t.Errorf("attempt 1: got %v", err)
	}

	// Attempt 2
	_, err = gm.VerifyGrant(email, "2222-2222")
	if !errors.Is(err, ErrInvalidGrantCode) {
		t.Errorf("attempt 2: got %v", err)
	}

	// Attempt 3: Exceeds max attempts and gets purged
	_, err = gm.VerifyGrant(email, "3333-3333")
	if !errors.Is(err, ErrGrantMaxAttempts) {
		t.Errorf("attempt 3: expected ErrGrantMaxAttempts, got %v", err)
	}

	// Subsequent lookup should be not found
	_, ok := gm.GetGrant(email)
	if ok {
		t.Errorf("expected grant to be purged after 3 failed attempts")
	}
}

func TestGrantManagerExpiration(t *testing.T) {
	gm := NewGrantManager()
	email := "expired@test.local"

	_, code, err := gm.CreateGrant(email, "admin", "admin@manova.space", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	_, err = gm.VerifyGrant(email, code)
	if !errors.Is(err, ErrGrantExpired) {
		t.Fatalf("expected ErrGrantExpired, got %v", err)
	}
}

func TestRegisterGrantWithCode(t *testing.T) {
	gm := NewGrantManager()
	email := "pre-set@test.local"

	rec, err := gm.RegisterGrantWithCode(email, "superadmin", "8492-0194", "admin@manova.space", 5*time.Minute)
	if err != nil {
		t.Fatalf("RegisterGrantWithCode failed: %v", err)
	}
	if rec.Role != "superadmin" {
		t.Errorf("expected superadmin, got %s", rec.Role)
	}

	verified, err := gm.VerifyGrant(email, "84920194")
	if err != nil {
		t.Fatalf("VerifyGrant with unhyphenated code failed: %v", err)
	}
	if verified.Role != "superadmin" {
		t.Errorf("expected superadmin, got %s", verified.Role)
	}
}

type mockGrantStore struct {
	grants map[string]*AdminGrant
}

func newMockGrantStore() *mockGrantStore {
	return &mockGrantStore{grants: make(map[string]*AdminGrant)}
}

func (m *mockGrantStore) SaveGrant(ctx context.Context, g *AdminGrant) error {
	m.grants[g.Email] = g
	return nil
}

func (m *mockGrantStore) GetGrant(ctx context.Context, email, codeHash string) (*AdminGrant, error) {
	g, ok := m.grants[email]
	if !ok {
		return nil, ErrGrantNotFound
	}
	if codeHash != "" && g.CodeHash != codeHash {
		return nil, ErrGrantNotFound
	}
	return g, nil
}

func (m *mockGrantStore) MarkUsed(ctx context.Context, id string) error {
	for _, g := range m.grants {
		if g.ID == id {
			g.Used = true
			now := time.Now().UTC()
			g.UsedAt = &now
			return nil
		}
	}
	return ErrGrantNotFound
}

func (m *mockGrantStore) ListActiveGrants(ctx context.Context) ([]*AdminGrant, error) {
	var list []*AdminGrant
	for _, g := range m.grants {
		if !g.Used && !g.IsExpired() {
			list = append(list, g)
		}
	}
	return list, nil
}

func TestPersistentGrantManager(t *testing.T) {
	mockStore := newMockGrantStore()
	gm := NewPersistentGrantManager(mockStore)

	if gm.Store() != GrantStore(mockStore) {
		t.Errorf("expected Store() to return configured mockStore")
	}

	email := "persisted@manova.space"
	rec, code, err := gm.CreateGrant(email, "admin", "root@manova.space", 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateGrant failed: %v", err)
	}

	// Verify grant is saved in store
	if _, ok := mockStore.grants[email]; !ok {
		t.Fatalf("expected grant to be persisted in mock store")
	}

	// Inspect via GetGrant
	inspected, ok := gm.GetGrant(email)
	if !ok || inspected.ID != rec.ID {
		t.Fatalf("GetGrant failed or ID mismatch")
	}

	// List active grants
	activeList, err := gm.ListActiveGrants()
	if err != nil {
		t.Fatalf("ListActiveGrants failed: %v", err)
	}
	if len(activeList) != 1 {
		t.Fatalf("expected 1 active grant, got %d", len(activeList))
	}

	// Verify valid code
	verified, err := gm.VerifyGrant(email, code)
	if err != nil {
		t.Fatalf("VerifyGrant failed: %v", err)
	}
	if !verified.Used {
		t.Errorf("expected grant to be marked used")
	}

	// Replay should fail
	_, err = gm.VerifyGrant(email, code)
	if !errors.Is(err, ErrGrantAlreadyUsed) && !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantAlreadyUsed or ErrGrantNotFound on replay, got %v", err)
	}
}

