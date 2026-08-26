package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/user"
)

func setupTestUserStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")
	mgr := user.NewUserManager(storePath)

	u1 := user.DeveloperUser{
		UID:         "alice",
		Email:       "alice@manova.space",
		DisplayName: "Alice Smith",
		Role:        "admin",
		Status:      user.StatusActive,
		WireGuardIP: "10.88.0.2",
		ForgejoUser: "alice",
		SSHKeyCount: 2,
		CreatedAt:   time.Now().UTC(),
	}
	u2 := user.DeveloperUser{
		UID:         "bob",
		Email:       "bob@manova.space",
		DisplayName: "Bob Jones",
		Role:        "developer",
		Status:      user.StatusLocked,
		LockReason:  "Audit pending",
		WireGuardIP: "10.88.0.3",
		ForgejoUser: "bob",
		SSHKeyCount: 1,
		CreatedAt:   time.Now().UTC(),
	}

	_ = mgr.SaveUser(context.Background(), u1)
	_ = mgr.SaveUser(context.Background(), u2)

	return storePath
}

func TestUserListCmd(t *testing.T) {
	storePath := setupTestUserStore(t)

	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "list", "--store", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("user list failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "alice") || !strings.Contains(out, "bob") {
		t.Errorf("expected both users in table output, got:\n%s", out)
	}
	if !strings.Contains(out, "10.88.0.2") {
		t.Errorf("expected WireGuard IP in table, got:\n%s", out)
	}
}

func TestUserInspectCmd(t *testing.T) {
	storePath := setupTestUserStore(t)

	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "inspect", "alice", "--store", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("user inspect failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Alice Smith") || !strings.Contains(out, "LLDAP Directory") {
		t.Errorf("expected detailed profile, got:\n%s", out)
	}
}

func TestUserLockAndUnlockCmd(t *testing.T) {
	storePath := setupTestUserStore(t)

	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Lock
	cmd.SetArgs([]string{"user", "lock", "alice", "--reason", "Security check", "--store", storePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("user lock failed: %v", err)
	}

	mgr := user.NewUserManager(storePath)
	u, _ := mgr.GetUser(context.Background(), "alice")
	if u.Status != user.StatusLocked || u.LockReason != "Security check" {
		t.Errorf("expected user alice to be locked, got: %+v", u)
	}

	// Unlock
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "unlock", "alice", "--store", storePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("user unlock failed: %v", err)
	}

	u, _ = mgr.GetUser(context.Background(), "alice")
	if u.Status != user.StatusActive {
		t.Errorf("expected user alice to be active, got: %+v", u)
	}
}

func TestUserDeprovisionCmd(t *testing.T) {
	storePath := setupTestUserStore(t)

	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "deprovision", "bob", "--yes", "--store", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("user deprovision failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "offboarded successfully") || !strings.Contains(out, "10.88.0.3") {
		t.Errorf("expected deprovision summary in output, got:\n%s", out)
	}

	mgr := user.NewUserManager(storePath)
	_, err := mgr.GetUser(context.Background(), "bob")
	if err != user.ErrUserNotFound {
		t.Errorf("expected user bob to be removed from store, got: %v", err)
	}
}

func TestUserRotateKeyCmd(t *testing.T) {
	storePath := setupTestUserStore(t)

	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "rotate-key", "alice", "--store", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("user rotate-key failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Key Rotation Token for alice@manova.space") || !strings.Contains(out, "orbit onboard --token") {
		t.Errorf("expected rotate-key instructions in output, got:\n%s", out)
	}
}
