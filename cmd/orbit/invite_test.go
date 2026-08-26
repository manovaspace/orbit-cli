package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
)

func TestInviteCreateCmd(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "alex@example.com",
		"--name", "Alex Smith",
		"--scope", "core",
		"--expires", "48h",
		"--store-file", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite create failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Developer Invitation Generated") {
		t.Errorf("output missing invitation header: %s", out)
	}
	if !strings.Contains(out, "alex@example.com") {
		t.Errorf("output missing email: %s", out)
	}
	if !strings.Contains(out, "Alex Smith") {
		t.Errorf("output missing display name: %s", out)
	}
	if !strings.Contains(out, "orbit onboard --token") {
		t.Errorf("output missing onboarding instructions: %s", out)
	}

	// Verify saved to store
	store, err := invite.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	records, err := store.ListInvites()
	if err != nil {
		t.Fatalf("ListInvites failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 saved invite, got %d", len(records))
	}
	if records[0].Email != "alex@example.com" {
		t.Errorf("expected email alex@example.com, got %s", records[0].Email)
	}
	if records[0].DisplayName != "Alex Smith" {
		t.Errorf("expected name Alex Smith, got %s", records[0].DisplayName)
	}
	if records[0].Scope != "core" {
		t.Errorf("expected scope core, got %s", records[0].Scope)
	}

	// Verify token is valid
	claims, err := invite.ValidateToken(records[0].Token, []byte(DefaultFallbackSecret))
	if err != nil {
		t.Fatalf("ValidateToken on generated token failed: %v", err)
	}
	if claims.Email != "alex@example.com" {
		t.Errorf("claims email mismatch: %s", claims.Email)
	}
}

func TestInviteCreateWithSecretEnv(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	customSecret := "custom-secret-key-at-least-32-bytes-long!"
	t.Setenv("TEST_INVITE_SECRET", customSecret)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "sarah@example.com",
		"--secret-env", "TEST_INVITE_SECRET",
		"--expires", "7d",
		"--store-file", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite create with secret-env failed: %v", err)
	}

	store, err := invite.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	records, err := store.ListInvites()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %v (err: %v)", len(records), err)
	}

	// Validate token with custom secret -> should pass
	claims, err := invite.ValidateToken(records[0].Token, []byte(customSecret))
	if err != nil {
		t.Fatalf("expected token to validate with custom secret: %v", err)
	}
	if claims.Email != "sarah@example.com" {
		t.Errorf("expected email sarah@example.com, got %s", claims.Email)
	}

	// Validate token with default secret -> should fail
	_, err = invite.ValidateToken(records[0].Token, []byte(DefaultFallbackSecret))
	if err == nil {
		t.Fatal("expected token validation to fail with wrong secret, got nil")
	}
}

func TestInviteCreateInvalidArgs(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")

	// Missing email arg
	cmd := newRootCmd()
	cmd.SetArgs([]string{"invite", "create"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing email arg, got nil")
	}

	// Invalid email
	buf := new(bytes.Buffer)
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "create", "not-an-email", "--store-file", storePath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid email, got nil")
	}

	// Invalid expires duration
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "create", "dev@example.com", "--expires", "invalid-duration", "--store-file", storePath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid expires, got nil")
	}
}

func TestInviteListCmd(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")

	// 1. List when empty
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--store-file", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite list empty failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No invitations found") {
		t.Errorf("expected 'No invitations found', got: %s", buf.String())
	}

	// 2. Create two invites
	cmd = newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "alex@example.com", "--name", "Alex", "--store-file", storePath})
	_ = cmd.Execute()

	cmd = newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "bob@example.com", "--name", "Bob", "--scope", "client", "--store-file", storePath})
	_ = cmd.Execute()

	// 3. Table format list
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--store-file", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite list table failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Developer Invitations") {
		t.Errorf("output missing table title: %s", out)
	}
	if !strings.Contains(out, "alex@example.com") || !strings.Contains(out, "bob@example.com") {
		t.Errorf("output missing emails: %s", out)
	}
	if !strings.Contains(out, "active") {
		t.Errorf("output missing active status: %s", out)
	}

	// 4. JSON format list
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--format", "json", "--store-file", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite list json failed: %v", err)
	}

	var jsonRecords []invite.InviteRecord
	if err := json.Unmarshal(buf.Bytes(), &jsonRecords); err != nil {
		t.Fatalf("failed to unmarshal JSON list output: %v\nOutput: %s", err, buf.String())
	}
	if len(jsonRecords) != 2 {
		t.Fatalf("expected 2 JSON records, got %d", len(jsonRecords))
	}
}

func TestInviteRevokeCmd(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")

	// 1. Create an invite
	cmd := newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "claire@example.com", "--store-file", storePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite create failed: %v", err)
	}

	store, _ := invite.NewStore(storePath)
	records, _ := store.ListInvites()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	inviteID := records[0].ID

	// 2. Revoke by ID prefix
	buf := new(bytes.Buffer)
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "revoke", inviteID[:10], "--store-file", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite revoke by ID prefix failed: %v", err)
	}

	if !strings.Contains(buf.String(), "Revoked invitation") {
		t.Errorf("expected 'Revoked invitation' in output: %s", buf.String())
	}

	// Verify in store
	reloaded, _ := store.GetInvite(inviteID)
	if !reloaded.Revoked || reloaded.Status() != "revoked" {
		t.Errorf("expected record to be revoked, got %s (revoked=%v)", reloaded.Status(), reloaded.Revoked)
	}

	// 3. Create another invite and revoke by full token string
	cmd = newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "dan@example.com", "--store-file", storePath})
	_ = cmd.Execute()

	records, _ = store.ListInvites()
	var danToken string
	for _, r := range records {
		if r.Email == "dan@example.com" {
			danToken = r.Token
		}
	}

	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "revoke", danToken, "--store-file", storePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite revoke by token failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Revoked invitation") {
		t.Errorf("expected 'Revoked invitation' in output: %s", buf.String())
	}

	// 4. Revoking nonexistent ID returns error
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "revoke", "nonexistent-id-12345", "--store-file", storePath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error revoking nonexistent invite, got nil")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		hasErr   bool
	}{
		{"", 7 * 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"14d", 14 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"168h", 168 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"0d", 0, true},
		{"-5h", 0, true},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		got, err := parseDuration(tc.input)
		if tc.hasErr && err == nil {
			t.Errorf("parseDuration(%q) expected error, got nil", tc.input)
		}
		if !tc.hasErr && err != nil {
			t.Errorf("parseDuration(%q) unexpected error: %v", tc.input, err)
		}
		if !tc.hasErr && got != tc.expected {
			t.Errorf("parseDuration(%q) = %v, expected %v", tc.input, got, tc.expected)
		}
	}
}
