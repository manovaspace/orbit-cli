package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner"
)

func TestAdminInitFailsWithoutSMTP(t *testing.T) {
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd := newAdminInitCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"--owner", "test@example.com", "--store", filepath.Join(t.TempDir(), "owner.json")})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected admin init to fail when SMTP is unconfigured")
	}

	combined := buf.String() + errBuf.String()
	if strings.Contains(combined, "Verification OTP generated") {
		t.Fatalf("security violation: OTP was printed to terminal: %s", combined)
	}
	if !strings.Contains(combined, "Mail relay is not configured") && !strings.Contains(combined, "Pre-flight Check Failed") {
		t.Fatalf("expected pre-flight error message, got: %s", combined)
	}
}

func TestAdminInit_NoSendWithoutCodeFails(t *testing.T) {
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd := newAdminInitCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"--owner", "test@example.com", "--no-send", "--store", filepath.Join(t.TempDir(), "owner.json")})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected admin init to fail when --no-send is used without --code")
	}

	combined := buf.String() + errBuf.String()
	if strings.Contains(combined, "Verification OTP generated") {
		t.Fatalf("security violation: OTP was printed to terminal: %s", combined)
	}
}

func TestAdminInit_SuccessWithCodeAndNoSend(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "alirezaopmc@gmail.com",
		"--name", "Alireza",
		"--code", "123456",
		"--no-send",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Platform Ownership Verified") {
		t.Errorf("output missing verification title: %s", out)
	}
	if !strings.Contains(out, "alirezaopmc@gmail.com") {
		t.Errorf("output missing owner email: %s", out)
	}
	if !strings.Contains(out, "Alireza") {
		t.Errorf("output missing display name: %s", out)
	}
	if !strings.Contains(out, "0600 sealed") {
		t.Errorf("output missing vault permissions notice: %s", out)
	}

	store := owner.NewStore(storePath)
	if !store.IsVerified() {
		t.Fatalf("expected store to be verified")
	}

	rec, err := store.LoadOwner()
	if err != nil {
		t.Fatalf("LoadOwner failed: %v", err)
	}
	if rec.Email != "alirezaopmc@gmail.com" {
		t.Errorf("expected email alirezaopmc@gmail.com, got %s", rec.Email)
	}
	if rec.DisplayName != "Alireza" {
		t.Errorf("expected name Alireza, got %s", rec.DisplayName)
	}
	if len(rec.RootSigningSecret) != 64 {
		t.Errorf("expected 64-character hex secret, got len %d", len(rec.RootSigningSecret))
	}
	if rec.KeyFingerprint == "" {
		t.Errorf("expected non-empty key fingerprint")
	}

	// Verify file permissions are 0600
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 file permissions, got %o", perm)
	}
}

func TestAdminInit_AlreadyVerified(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	rec := &owner.OwnerRecord{
		Email:             "alirezaopmc@gmail.com",
		DisplayName:       "Alireza",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: "existingsecret12345678901234567890123456789012345678901234567890",
		KeyFingerprint:    "fingerprint123",
	}
	if err := store.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "alirezaopmc@gmail.com",
		"--code", "999999",
		"--no-send",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "already verified") {
		t.Errorf("expected output to contain 'already verified', got: %s", out)
	}

	// Secret should not have changed
	loaded, err := store.LoadOwner()
	if err != nil {
		t.Fatalf("LoadOwner failed: %v", err)
	}
	if loaded.RootSigningSecret != rec.RootSigningSecret {
		t.Errorf("secret changed without --force")
	}
}

func TestAdminInit_ForceReInit(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	oldRec := &owner.OwnerRecord{
		Email:             "old@example.com",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: "oldsecret12345678901234567890123456789012345678901234567890",
	}
	if err := store.SaveOwner(oldRec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "alirezaopmc@gmail.com",
		"--code", "654321",
		"--no-send",
		"--force",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init --force failed: %v", err)
	}

	loaded, err := store.LoadOwner()
	if err != nil {
		t.Fatalf("LoadOwner failed: %v", err)
	}
	if loaded.Email != "alirezaopmc@gmail.com" {
		t.Errorf("expected updated email alirezaopmc@gmail.com, got %s", loaded.Email)
	}
	if loaded.RootSigningSecret == oldRec.RootSigningSecret {
		t.Errorf("expected secret to be regenerated")
	}
}

func TestAdminInit_InvalidEmail(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "invalid-email-no-at",
		"--code", "123456",
		"--no-send",
		"--store", storePath,
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid email, got nil")
	}
}

func TestAdminInit_InteractivePrompt(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	in := bytes.NewBufferString("interactive-owner@manova.space\n")

	cmd := newRootCmd()
	cmd.SetIn(in)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--code", "112233",
		"--no-send",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init interactive prompt failed: %v", err)
	}

	store := owner.NewStore(storePath)
	if !store.IsVerified() {
		t.Fatalf("expected store to be verified")
	}

	rec, _ := store.LoadOwner()
	if rec.Email != "interactive-owner@manova.space" {
		t.Errorf("expected interactive email, got %s", rec.Email)
	}
}

func TestAdminInit_WithMockSMTPServer(t *testing.T) {
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
	t.Setenv("ORBIT_SMTP_HOST", host)
	t.Setenv("ORBIT_SMTP_PORT", port)

	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "alirezaopmc@gmail.com",
		"--code", "748291",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init with SMTP dispatch failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Verification challenge dispatched to alirezaopmc@gmail.com") {
		t.Errorf("output missing dispatch confirmation: %s", out)
	}

	<-done
	payload := strings.Join(receivedEmail, "")
	if !strings.Contains(payload, "748291") {
		t.Errorf("dispatched email payload missing OTP code: %s", payload)
	}
	if !strings.Contains(payload, "alirezaopmc@gmail.com") {
		t.Errorf("dispatched email payload missing recipient: %s", payload)
	}
}

func TestAdminStatus_Unverified(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "status",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin status failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "UNVERIFIED") {
		t.Errorf("expected UNVERIFIED status, got: %s", out)
	}
	if !strings.Contains(out, "admin init --owner") {
		t.Errorf("expected suggestion to run init: %s", out)
	}
}

func TestAdminStatus_VerifiedTable(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	rec := &owner.OwnerRecord{
		Email:             "alirezaopmc@gmail.com",
		DisplayName:       "Alireza",
		VerifiedAt:        time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC),
		RootSigningSecret: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		KeyFingerprint:    "a1b2c3d4e5f60718",
	}
	if err := store.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "status",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin status failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "VERIFIED") {
		t.Errorf("expected VERIFIED in status output: %s", out)
	}
	if !strings.Contains(out, "alirezaopmc@gmail.com") {
		t.Errorf("expected owner email in output: %s", out)
	}
	if !strings.Contains(out, "Alireza") {
		t.Errorf("expected display name in output: %s", out)
	}
	if !strings.Contains(out, "a1b2c3d4e5f60718") {
		t.Errorf("expected key fingerprint in output: %s", out)
	}
	if !strings.Contains(out, "0600 (secure)") {
		t.Errorf("expected secure permissions output: %s", out)
	}
}

func TestAdminStatus_VerifiedJSON(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	rec := &owner.OwnerRecord{
		Email:             "alirezaopmc@gmail.com",
		DisplayName:       "Alireza",
		VerifiedAt:        time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC),
		RootSigningSecret: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		KeyFingerprint:    "a1b2c3d4e5f60718",
	}
	if err := store.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "status",
		"--store", storePath,
		"--format", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin status --format json failed: %v", err)
	}

	var res adminStatusJSON
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON output: %v, output: %s", err, buf.String())
	}

	if !res.Verified {
		t.Errorf("expected verified == true")
	}
	if res.Email != "alirezaopmc@gmail.com" {
		t.Errorf("expected email alirezaopmc@gmail.com, got %s", res.Email)
	}
	if res.DisplayName != "Alireza" {
		t.Errorf("expected display name Alireza, got %s", res.DisplayName)
	}
	if res.KeyFingerprint != "a1b2c3d4e5f60718" {
		t.Errorf("expected fingerprint a1b2c3d4e5f60718, got %s", res.KeyFingerprint)
	}
	if !res.PermissionsValid {
		t.Errorf("expected permissions_valid == true")
	}
	if res.VaultPermissions != "0600 (secure)" {
		t.Errorf("expected vault permissions '0600 (secure)', got %s", res.VaultPermissions)
	}
}

func TestAdminStatus_InsecurePermissions(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	rec := &owner.OwnerRecord{
		Email:             "test@example.com",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: "secret123",
	}
	if err := store.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	// Change permission to 0644 (insecure)
	if err := os.Chmod(storePath, 0644); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "status",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin status failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "insecure") {
		t.Errorf("expected insecure warning in status output: %s", out)
	}
}

func TestAdminVerify_Success(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "verify",
		"verified-owner@manova.space",
		"654321",
		"--name", "Verified Lead",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin verify failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Platform Ownership Verified") {
		t.Errorf("output missing verified header: %s", out)
	}
	if !strings.Contains(out, "verified-owner@manova.space") {
		t.Errorf("output missing verified email: %s", out)
	}

	store := owner.NewStore(storePath)
	if !store.IsVerified() {
		t.Fatalf("expected store to be verified after verify command")
	}

	rec, _ := store.LoadOwner()
	if rec.DisplayName != "Verified Lead" {
		t.Errorf("expected display name 'Verified Lead', got %s", rec.DisplayName)
	}
}

func TestAdminVerify_AlreadyVerified(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	rec := &owner.OwnerRecord{
		Email:             "already@example.com",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: "existingsecret123",
	}
	_ = store.SaveOwner(rec)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "verify",
		"already@example.com",
		"112233",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin verify failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "already verified") {
		t.Errorf("expected 'already verified' in output: %s", out)
	}
}

func TestAdminRotateSecret_Success(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	origSecret, _ := owner.GenerateMasterSecret()
	rec := &owner.OwnerRecord{
		Email:             "alirezaopmc@gmail.com",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: origSecret,
		KeyFingerprint:    owner.ComputeFingerprint(origSecret),
	}
	if err := store.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "rotate-secret",
		"--store", storePath,
		"--yes",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin rotate-secret failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Master Signing Secret Rotated") {
		t.Errorf("output missing rotation title: %s", out)
	}
	if !strings.Contains(out, "invalidated") {
		t.Errorf("output missing invalidation warning: %s", out)
	}

	reloaded, err := store.LoadOwner()
	if err != nil {
		t.Fatalf("LoadOwner failed: %v", err)
	}
	if reloaded.RootSigningSecret == origSecret {
		t.Fatalf("expected new secret after rotation")
	}
	if reloaded.KeyFingerprint == rec.KeyFingerprint {
		t.Fatalf("expected updated key fingerprint after rotation")
	}
}

func TestAdminRotateSecret_PromptCancel(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	origSecret, _ := owner.GenerateMasterSecret()
	rec := &owner.OwnerRecord{
		Email:             "alirezaopmc@gmail.com",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: origSecret,
	}
	_ = store.SaveOwner(rec)

	buf := new(bytes.Buffer)
	in := bytes.NewBufferString("n\n")

	cmd := newRootCmd()
	cmd.SetIn(in)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "rotate-secret",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin rotate-secret failed: %v", err)
	}

	if !strings.Contains(buf.String(), "cancelled") {
		t.Errorf("expected cancellation message: %s", buf.String())
	}

	reloaded, _ := store.LoadOwner()
	if reloaded.RootSigningSecret != origSecret {
		t.Errorf("secret changed despite cancellation")
	}
}

func TestAdminRotateSecret_UnverifiedError(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "rotate-secret",
		"--store", storePath,
		"--yes",
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error rotating secret when unverified, got nil")
	}
}
