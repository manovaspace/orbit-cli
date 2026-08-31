package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
)

func createTestVerifiedOwnerStore(t *testing.T, dir string) (string, *owner.OwnerRecord) {
	t.Helper()
	storePath := filepath.Join(dir, "owner.json")
	store := owner.NewStore(storePath)
	secret, err := owner.GenerateMasterSecret()
	if err != nil {
		t.Fatalf("failed to generate master secret: %v", err)
	}
	rec := &owner.OwnerRecord{
		Email:             "admin@example.com",
		DisplayName:       "Admin User",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: secret,
		KeyFingerprint:    owner.ComputeFingerprint(secret),
	}
	if err := store.SaveOwner(rec); err != nil {
		t.Fatalf("failed to save test owner record: %v", err)
	}
	return storePath, rec
}

func TestInviteOwnerGuard_Unverified(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	unverifiedOwnerPath := filepath.Join(tempDir, "nonexistent-owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "unverified-test@example.com",
		"--store-file", storePath,
		"--owner-store", unverifiedOwnerPath,
		"--no-send",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error creating invite when owner is unverified, got nil")
	}

	expectedErr := "platform ownership is unverified. Run 'orbit admin init --owner <email>' to verify ownership before issuing invitations."
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestInviteOwnerGuard_InsecureBypass(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	unverifiedOwnerPath := filepath.Join(tempDir, "nonexistent-owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "bypassed@example.com",
		"--insecure",
		"--store-file", storePath,
		"--owner-store", unverifiedOwnerPath,
		"--no-send",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected --insecure to bypass unverified owner check, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Developer Invitation Generated") {
		t.Errorf("output missing invitation header: %s", out)
	}
	if !strings.Contains(out, "bypassed@example.com") {
		t.Errorf("output missing email: %s", out)
	}
}

func TestInviteOwnerGuard_VerifiedOwner(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, ownerRec := createTestVerifiedOwnerStore(t, tempDir)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "newdev@example.com",
		"--name", "New Developer",
		"--scope", "core",
		"--store-file", storePath,
		"--owner-store", ownerPath,
		"--no-send",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite create with verified owner failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Developer Invitation Generated") {
		t.Errorf("output missing invitation header: %s", out)
	}
	if !strings.Contains(out, "Created By:") || !strings.Contains(out, "admin@example.com") {
		t.Errorf("output missing Created By line: %s", out)
	}

	store, err := invite.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	records, err := store.ListInvites()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 saved invite, got %v (err: %v)", len(records), err)
	}

	if records[0].CreatedBy != ownerRec.Email {
		t.Errorf("expected CreatedBy %s, got %s", ownerRec.Email, records[0].CreatedBy)
	}
	if records[0].Email != "newdev@example.com" {
		t.Errorf("expected email newdev@example.com, got %s", records[0].Email)
	}

	claims, err := invite.ValidateToken(records[0].Token, []byte(ownerRec.RootSigningSecret))
	if err != nil {
		t.Fatalf("ValidateToken with owner root secret failed: %v", err)
	}
	if claims.ID != records[0].ID {
		t.Errorf("claims ID mismatch: %s vs %s", claims.ID, records[0].ID)
	}
}

func TestInviteCreateCmd(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, ownerRec := createTestVerifiedOwnerStore(t, tempDir)

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
		"--owner-store", ownerPath,
		"--no-send",
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
	if !strings.Contains(out, "Created By:") || !strings.Contains(out, ownerRec.Email) {
		t.Errorf("output missing Created By field: %s", out)
	}
	if !strings.Contains(out, "Web Setup:") || !strings.Contains(out, "https://orbit.manova.space/setup?token=orbit-inv.") {
		t.Errorf("output missing Web Setup line: %s", out)
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
	if records[0].CreatedBy != ownerRec.Email {
		t.Errorf("expected CreatedBy %s, got %s", ownerRec.Email, records[0].CreatedBy)
	}

	if !strings.HasPrefix(records[0].Token, "orbit-inv.") {
		t.Fatalf("expected signed token with prefix orbit-inv., got %s", records[0].Token)
	}
	claims, err := invite.ValidateToken(records[0].Token, []byte(ownerRec.RootSigningSecret))
	if err != nil {
		t.Fatalf("ValidateToken on generated token failed: %v", err)
	}
	if claims.ID != records[0].ID {
		t.Errorf("claims ID mismatch: %s vs %s", claims.ID, records[0].ID)
	}
}

func TestInviteCreateWithStorePath(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, _ := createTestVerifiedOwnerStore(t, tempDir)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "sarah@example.com",
		"--expires", "7d",
		"--store-file", storePath,
		"--owner-store", ownerPath,
		"--no-send",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite create failed: %v", err)
	}

	store, err := invite.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	records, err := store.ListInvites()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 record, got %v (err: %v)", len(records), err)
	}
	if records[0].Email != "sarah@example.com" {
		t.Errorf("expected email sarah@example.com, got %s", records[0].Email)
	}
	if !strings.HasPrefix(records[0].Token, "orbit-inv.") {
		t.Errorf("expected signed token prefix 'orbit-inv.', got %s", records[0].Token)
	}
}

func TestInviteCreateInvalidArgs(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, _ := createTestVerifiedOwnerStore(t, tempDir)

	// Missing email arg
	cmd := newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "--owner-store", ownerPath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing email arg, got nil")
	}

	// Invalid email
	buf := new(bytes.Buffer)
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "create", "not-an-email", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid email, got nil")
	}

	// Invalid expires duration
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "create", "dev@example.com", "--expires", "invalid-duration", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid expires, got nil")
	}
}

func TestInviteListCmd(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, _ := createTestVerifiedOwnerStore(t, tempDir)

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
	cmd.SetArgs([]string{"invite", "create", "alex@example.com", "--name", "Alex", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
	_ = cmd.Execute()

	cmd = newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "bob@example.com", "--name", "Bob", "--scope", "client", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
	_ = cmd.Execute()

	// 3. Table format list (both active)
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

	// 5. Revoke one invite (Alex)
	store, _ := invite.NewStore(storePath)
	records, _ := store.ListInvites()
	var alexID string
	for _, r := range records {
		if r.Email == "alex@example.com" {
			alexID = r.ID
		}
	}
	_, _ = store.RevokeInvite(alexID)

	// Default list should now only show 1 active invite (Bob), but summary shows total 2, 1 active, 1 revoked
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--store-file", storePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite list with 1 active failed: %v", err)
	}
	out = buf.String()
	if strings.Contains(out, "alex@example.com") {
		t.Errorf("expected alex (revoked) to be hidden from default list view: %s", out)
	}
	if !strings.Contains(out, "bob@example.com") {
		t.Errorf("expected bob (active) to be shown: %s", out)
	}
	if !strings.Contains(out, "Total: 2") || !strings.Contains(out, "1 active") || !strings.Contains(out, "1 revoked") {
		t.Errorf("expected summary stats to show 2 total, 1 active, 1 revoked: %s", out)
	}

	// List with --all should show both Alex (revoked) and Bob (active)
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--all", "--store-file", storePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite list --all failed: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "alex@example.com") || !strings.Contains(out, "bob@example.com") {
		t.Errorf("expected both alex and bob in --all view: %s", out)
	}
	if !strings.Contains(out, "revoked") || !strings.Contains(out, "active") {
		t.Errorf("expected both active and revoked statuses in --all view: %s", out)
	}

	// JSON format with --all should return 2 records, without --all returns 1 record
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--format", "json", "--store-file", storePath})
	_ = cmd.Execute()
	var jsonActiveOnly []invite.InviteRecord
	_ = json.Unmarshal(buf.Bytes(), &jsonActiveOnly)
	if len(jsonActiveOnly) != 1 {
		t.Fatalf("expected 1 active JSON record, got %d", len(jsonActiveOnly))
	}

	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--format", "json", "--all", "--store-file", storePath})
	_ = cmd.Execute()
	var jsonAll []invite.InviteRecord
	_ = json.Unmarshal(buf.Bytes(), &jsonAll)
	if len(jsonAll) != 2 {
		t.Fatalf("expected 2 JSON records with --all, got %d", len(jsonAll))
	}

	// When all invites are revoked, default list shows friendly message
	var bobID string
	for _, r := range records {
		if r.Email == "bob@example.com" {
			bobID = r.ID
		}
	}
	_, _ = store.RevokeInvite(bobID)
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "list", "--store-file", storePath})
	_ = cmd.Execute()
	if !strings.Contains(buf.String(), "No active invitations found") {
		t.Errorf("expected 'No active invitations found' message, got: %s", buf.String())
	}
}

func TestInviteRevokeCmd(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, _ := createTestVerifiedOwnerStore(t, tempDir)

	// 1. Create an invite
	cmd := newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "claire@example.com", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
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
	cmd.SetArgs([]string{"invite", "revoke", inviteID[:6], "--store-file", storePath})

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
	cmd.SetArgs([]string{"invite", "create", "dan@example.com", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
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

	// 5. Revoking without args and without --all returns error
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "revoke", "--store-file", storePath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error revoking without args or --all, got nil")
	}

	// 6. Revoking with positional arg AND --all returns error
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "revoke", "some-token", "--all", "--store-file", storePath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when combining positional arg with --all, got nil")
	}

	// 7. Test bulk revoke --all
	// Create two new active invites
	cmd = newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "dev1@example.com", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
	_ = cmd.Execute()
	cmd = newRootCmd()
	cmd.SetArgs([]string{"invite", "create", "dev2@example.com", "--store-file", storePath, "--owner-store", ownerPath, "--no-send"})
	_ = cmd.Execute()

	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "revoke", "--all", "--store-file", storePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite revoke --all failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Invitations Revoked") || !strings.Contains(out, "Revoked 2 active invitation(s)") {
		t.Errorf("expected 2 active invitations revoked in output: %s", out)
	}
	if !strings.Contains(out, "dev1@example.com") || !strings.Contains(out, "dev2@example.com") {
		t.Errorf("expected dev1 and dev2 listed in bulk revoke output: %s", out)
	}

	// 8. Bulk revoke when none are active
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"invite", "revoke", "--all", "--store-file", storePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite revoke --all with 0 active failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No active invitations to revoke") {
		t.Errorf("expected 'No active invitations to revoke' message, got: %s", buf.String())
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

func TestInviteCreateWithSendFlag(t *testing.T) {
	// 1. Start mock SMTP server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock smtp listener: %v", err)
	}
	defer l.Close()

	var receivedEmail []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		_, _ = w.WriteString("220 mock.smtp.orbit Service Ready\r\n")
		_ = w.Flush()

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			cmd := strings.ToUpper(fields[0])

			switch cmd {
			case "HELO", "EHLO":
				_, _ = w.WriteString("250-mock.smtp.orbit\r\n250 HELP\r\n")
				_ = w.Flush()
			case "MAIL", "RCPT":
				_, _ = w.WriteString("250 OK\r\n")
				_ = w.Flush()
			case "DATA":
				_, _ = w.WriteString("354 Start mail input; end with <CRLF>.<CRLF>\r\n")
				_ = w.Flush()
				for {
					dataLine, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if dataLine == ".\r\n" || dataLine == ".\n" {
						break
					}
					receivedEmail = append(receivedEmail, dataLine)
				}
				_, _ = w.WriteString("250 OK: queued\r\n")
				_ = w.Flush()
			case "QUIT":
				_, _ = w.WriteString("221 Bye\r\n")
				_ = w.Flush()
				return
			}
		}
	}()

	host, port, _ := net.SplitHostPort(l.Addr().String())
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, _ := createTestVerifiedOwnerStore(t, tempDir)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "john@example.com",
		"--name", "John Doe",
		"--send",
		"--smtp-host", host,
		"--smtp-port", port,
		"--store-file", storePath,
		"--owner-store", ownerPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite create --send failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Invitation email dispatched to john@example.com") {
		t.Errorf("output missing dispatch confirmation: %s", out)
	}

	<-done
	payload := strings.Join(receivedEmail, "")
	if !strings.Contains(payload, "John Doe") {
		t.Errorf("received email payload missing developer name: %s", payload)
	}
	if !strings.Contains(payload, "john@example.com") {
		t.Errorf("received email payload missing recipient: %s", payload)
	}
}

func TestInviteCreateWithNoSendFlag(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	ownerPath, _ := createTestVerifiedOwnerStore(t, tempDir)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"invite", "create", "nosend@example.com",
		"--no-send",
		"--store-file", storePath,
		"--owner-store", ownerPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("invite create --no-send failed: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Invitation email dispatched") {
		t.Errorf("expected no email dispatch with --no-send: %s", out)
	}
}

func TestInviteCreateHeadlessMissingFlags(t *testing.T) {
	cmd := newInviteCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "--insecure"}) // No email flag, no -i

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected command to fail fast when required flags are missing in headless mode")
	}
	expectedMsg := `required flag(s) "email" not set (use -i for interactive mode)`
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Fatalf("expected error message to contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestInviteCreateInteractiveFlagRegistered(t *testing.T) {
	cmd := newInviteCmd()
	createCmd, _, err := cmd.Find([]string{"create"})
	if err != nil {
		t.Fatalf("failed to find create subcommand: %v", err)
	}

	interactiveFlag := createCmd.Flags().Lookup("interactive")
	if interactiveFlag == nil {
		t.Fatal("expected --interactive / -i flag to be registered on invite create")
	}
	if interactiveFlag.Shorthand != "i" {
		t.Errorf("expected shorthand 'i', got %q", interactiveFlag.Shorthand)
	}
}

func TestInviteCreateInteractiveNonTTY(t *testing.T) {
	cmd := newInviteCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "--interactive", "--insecure"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when interactive mode is run in non-interactive environment")
	}
	expectedErr := "interactive mode requires an active terminal session"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestInviteCreateInteractiveShorthandNonTTY(t *testing.T) {
	cmd := newInviteCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "-i", "--insecure"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when interactive mode is run in non-interactive environment")
	}
	expectedErr := "interactive mode requires an active terminal session"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestInviteTokenMetadata_CreateAlias(t *testing.T) {
	req := invite.InviteRequest{
		Email:       "test-alias@example.com",
		DisplayName: "Alias User",
		Scope:       "dev",
		TTL:         72 * time.Hour,
		CreatedBy:   "owner@manova.space",
		Metadata: map[string]string{
			"create_alias": "true",
		},
	}

	tokenStr, claims, err := invite.GenerateToken(req, []byte(DefaultFallbackSecret))
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if claims.Metadata == nil || claims.Metadata["create_alias"] != "true" {
		t.Fatalf("expected claims.Metadata['create_alias'] = 'true', got: %v", claims.Metadata)
	}
	if claims.CreatedBy != "owner@manova.space" {
		t.Errorf("expected claims.CreatedBy 'owner@manova.space', got: %s", claims.CreatedBy)
	}

	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "invites.json")
	store, err := invite.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	rec := &invite.InviteRecord{
		ID:          claims.ID,
		Email:       claims.Email,
		DisplayName: claims.DisplayName,
		Scope:       claims.Scope,
		Token:       tokenStr,
		Revoked:     false,
		IssuedAt:    claims.IssuedAt,
		ExpiresAt:   claims.ExpiresAt,
		CreatedBy:   claims.CreatedBy,
		Metadata:    claims.Metadata,
	}
	if err := store.SaveInvite(rec); err != nil {
		t.Fatalf("SaveInvite failed: %v", err)
	}

	saved, err := store.GetInvite(claims.ID)
	if err != nil {
		t.Fatalf("GetInvite failed: %v", err)
	}
	if saved.Metadata == nil || saved.Metadata["create_alias"] != "true" {
		t.Errorf("expected stored metadata['create_alias'] = 'true', got: %v", saved.Metadata)
	}
	if saved.CreatedBy != "owner@manova.space" {
		t.Errorf("expected stored CreatedBy 'owner@manova.space', got: %s", saved.CreatedBy)
	}
}
