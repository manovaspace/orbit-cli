package invite

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInviteStoreLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// 1. Initial list is empty
	invites, err := store.ListInvites()
	if err != nil {
		t.Fatalf("ListInvites failed: %v", err)
	}
	if len(invites) != 0 {
		t.Fatalf("expected 0 invites, got %d", len(invites))
	}

	// 2. Save invites
	rec1 := &InviteRecord{
		ID:          "abcdef1234567890abcdef1234567890",
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Scope:       "core",
		Token:       "manova-inv.token1.sig1",
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(24 * time.Hour),
	}

	rec2 := &InviteRecord{
		ID:          "123456abcdef7890123456abcdef7890",
		Email:       "bob@example.com",
		DisplayName: "Bob",
		Scope:       "client",
		Token:       "manova-inv.token2.sig2",
		IssuedAt:    time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(48 * time.Hour),
	}

	if err := store.SaveInvite(rec1); err != nil {
		t.Fatalf("SaveInvite rec1 failed: %v", err)
	}
	if err := store.SaveInvite(rec2); err != nil {
		t.Fatalf("SaveInvite rec2 failed: %v", err)
	}

	// Verify file permissions (0600)
	fi, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("os.Stat failed: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %v", fi.Mode().Perm())
	}

	// 3. List
	all, err := store.ListInvites()
	if err != nil {
		t.Fatalf("ListInvites failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 invites, got %d", len(all))
	}

	// 4. Get by exact ID and prefix and Token
	got1, err := store.GetInvite("abcdef1234567890abcdef1234567890")
	if err != nil || got1.Email != "alice@example.com" {
		t.Fatalf("GetInvite by exact ID failed: %v", err)
	}

	gotPrefix, err := store.GetInvite("abcdef1234")
	if err != nil || gotPrefix.Email != "alice@example.com" {
		t.Fatalf("GetInvite by prefix failed: %v", err)
	}

	gotByToken, err := store.GetInvite("manova-inv.token2.sig2")
	if err != nil || gotByToken.Email != "bob@example.com" {
		t.Fatalf("GetInvite by token failed: %v", err)
	}

	// 5. Revoke
	revoked, err := store.RevokeInvite("abcdef1234")
	if err != nil {
		t.Fatalf("RevokeInvite failed: %v", err)
	}
	if !revoked.Revoked || revoked.Status() != "revoked" {
		t.Errorf("expected status revoked, got %s", revoked.Status())
	}

	// 6. Reload from new Store instance
	store2, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore store2 failed: %v", err)
	}
	reloaded, err := store2.GetInvite("abcdef1234")
	if err != nil {
		t.Fatalf("GetInvite on reloaded store failed: %v", err)
	}
	if !reloaded.Revoked {
		t.Errorf("expected persisted revoked state to be true")
	}

	// 7. Not found
	_, err = store.GetInvite("nonexistent-id-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent invite, got nil")
	}
	_, err = store.RevokeInvite("nonexistent-id-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent invite revocation, got nil")
	}
}

func TestInviteRecordStatus(t *testing.T) {
	now := time.Now().UTC()
	activeRec := &InviteRecord{
		Revoked:   false,
		ExpiresAt: now.Add(1 * time.Hour),
	}
	if activeRec.Status() != "active" {
		t.Errorf("expected active, got %s", activeRec.Status())
	}

	expiredRec := &InviteRecord{
		Revoked:   false,
		ExpiresAt: now.Add(-1 * time.Hour),
	}
	if expiredRec.Status() != "expired" {
		t.Errorf("expected expired, got %s", expiredRec.Status())
	}

	revokedRec := &InviteRecord{
		Revoked:   true,
		ExpiresAt: now.Add(1 * time.Hour),
	}
	if revokedRec.Status() != "revoked" {
		t.Errorf("expected revoked, got %s", revokedRec.Status())
	}
}
