package user_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/user"
)

func seedTestUsers(t *testing.T, mgr user.UserManager) {
	t.Helper()
	ctx := context.Background()

	u1 := user.DeveloperUser{
		UID:          "alice",
		Email:        "alice@manova.space",
		DisplayName:  "Alice Dev",
		Role:         "admin",
		Status:       user.StatusActive,
		WireGuardIP:  "10.88.0.2",
		ForgejoUser:  "alice",
		SSHKeyCount:  2,
		CreatedAt:    time.Now().Add(-48 * time.Hour),
	}
	u2 := user.DeveloperUser{
		UID:          "bob",
		Email:        "bob@manova.space",
		DisplayName:  "Bob Dev",
		Role:         "developer",
		Status:       user.StatusLocked,
		LockReason:   "Suspended for rotation",
		WireGuardIP:  "10.88.0.3",
		ForgejoUser:  "bob",
		SSHKeyCount:  1,
		CreatedAt:    time.Now().Add(-24 * time.Hour),
	}

	if err := mgr.SaveUser(ctx, u1); err != nil {
		t.Fatalf("failed to save user 1: %v", err)
	}
	if err := mgr.SaveUser(ctx, u2); err != nil {
		t.Fatalf("failed to save user 2: %v", err)
	}
}

func TestListUsers_Filtering(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")
	mgr := user.NewUserManager(storePath)
	seedTestUsers(t, mgr)

	ctx := context.Background()

	// 1. List All
	all, err := mgr.ListUsers(ctx, "all")
	if err != nil {
		t.Fatalf("ListUsers(all) failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 users in all, got %d", len(all))
	}

	// 2. List Active
	active, err := mgr.ListUsers(ctx, "active")
	if err != nil {
		t.Fatalf("ListUsers(active) failed: %v", err)
	}
	if len(active) != 1 || active[0].UID != "alice" {
		t.Errorf("expected only alice in active, got: %+v", active)
	}

	// 3. List Locked
	locked, err := mgr.ListUsers(ctx, "locked")
	if err != nil {
		t.Fatalf("ListUsers(locked) failed: %v", err)
	}
	if len(locked) != 1 || locked[0].UID != "bob" {
		t.Errorf("expected only bob in locked, got: %+v", locked)
	}
}

func TestGetUser_Lookup(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")
	mgr := user.NewUserManager(storePath)
	seedTestUsers(t, mgr)

	ctx := context.Background()

	// By UID
	u1, err := mgr.GetUser(ctx, "alice")
	if err != nil || u1.Email != "alice@manova.space" {
		t.Errorf("failed to lookup by UID: err=%v, u=%+v", err, u1)
	}

	// By Email (case insensitive)
	u2, err := mgr.GetUser(ctx, "BOB@manova.space")
	if err != nil || u2.UID != "bob" {
		t.Errorf("failed to lookup by email: err=%v, u=%+v", err, u2)
	}

	// Not found
	_, err = mgr.GetUser(ctx, "unknown@manova.space")
	if err != user.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestLockAndUnlockUser(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")
	mgr := user.NewUserManager(storePath)
	seedTestUsers(t, mgr)

	ctx := context.Background()

	// Lock alice
	if err := mgr.LockUser(ctx, "alice@manova.space", "Security review"); err != nil {
		t.Fatalf("LockUser failed: %v", err)
	}
	u, _ := mgr.GetUser(ctx, "alice")
	if u.Status != user.StatusLocked || u.LockReason != "Security review" {
		t.Errorf("expected alice to be locked with reason, got: %+v", u)
	}

	// Unlock alice
	if err := mgr.UnlockUser(ctx, "alice"); err != nil {
		t.Fatalf("UnlockUser failed: %v", err)
	}
	u, _ = mgr.GetUser(ctx, "alice")
	if u.Status != user.StatusActive || u.LockReason != "" {
		t.Errorf("expected alice to be active, got: %+v", u)
	}
}

func TestDeprovisionUser_AtomicCleanup(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")
	mgr := user.NewUserManager(storePath)
	seedTestUsers(t, mgr)

	ctx := context.Background()

	summary, err := mgr.DeprovisionUser(ctx, "bob@manova.space")
	if err != nil {
		t.Fatalf("DeprovisionUser failed: %v", err)
	}

	if summary.UID != "bob" || summary.WireGuardFreedIP != "10.88.0.3" || !summary.LldapRemoved || !summary.ForgejoRemoved {
		t.Errorf("unexpected deprovision summary: %+v", summary)
	}

	// Verify bob is gone
	_, err = mgr.GetUser(ctx, "bob")
	if err != user.ErrUserNotFound {
		t.Errorf("expected bob to be removed, got: %v", err)
	}
}

func TestRotateKey(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")
	mgr := user.NewUserManager(storePath)
	seedTestUsers(t, mgr)

	ctx := context.Background()

	tok, claims, err := mgr.RotateKey(ctx, "alice", []byte("test-signing-secret-key-32bytes-len"))
	if err != nil {
		t.Fatalf("RotateKey failed: %v", err)
	}

	if claims.Email != "alice@manova.space" || tok == "" {
		t.Errorf("unexpected rotate invite result: tok=%s, claims=%+v", tok, claims)
	}
}
