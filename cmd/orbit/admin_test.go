package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/owner"
)

func newMockAdminServer(t *testing.T, expectedEmail, validCode string) *httptest.Server {
	t.Helper()
	var challengeDispatched bool
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/admin/challenge", "/api/v1/system/ownership/challenge":
			var req client.ChallengeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json"})
				return
			}
			if req.Email != expectedEmail {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unexpected email"})
				return
			}
			challengeDispatched = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(client.ChallengeResponse{
				Status:    "pending",
				Email:     req.Email,
				ExpiresAt: time.Now().Add(10 * time.Minute),
				Message:   "OTP challenge code dispatched via SMTP",
			})
		case "/api/v1/admin/verify", "/api/v1/system/ownership/verify":
			if !challengeDispatched {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "no active challenge"})
				return
			}
			var req client.VerifyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid json"})
				return
			}
			if req.Email != expectedEmail || req.Code != validCode {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired OTP code"})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(client.VerifyResponse{
				Status:     "verified",
				Email:      req.Email,
				VerifiedAt: time.Now().UTC(),
				Message:    "Platform ownership verified",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	}))
}

func TestAdminInit_APIServer_Success(t *testing.T) {
	ts := newMockAdminServer(t, "admin@example.com", "748291")
	defer ts.Close()

	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "admin@example.com",
		"--name", "Alireza",
		"--code", "748291",
		"--server", ts.URL,
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init via API server failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Connecting to Orbit server at "+ts.URL) {
		t.Errorf("output missing server connection message: %s", out)
	}
	if !strings.Contains(out, "Challenge accepted for admin@example.com") {
		t.Errorf("output missing challenge dispatch notice: %s", out)
	}
	if !strings.Contains(out, "Orbit Platform Ownership Verified") {
		t.Errorf("output missing verification title: %s", out)
	}
	if !strings.Contains(out, "0600 sealed") {
		t.Errorf("output missing vault sealed notice: %s", out)
	}

	store := owner.NewStore(storePath)
	if !store.IsVerified() {
		t.Fatalf("expected store to be verified")
	}

	rec, err := store.LoadOwner()
	if err != nil {
		t.Fatalf("LoadOwner failed: %v", err)
	}
	if rec.Email != "admin@example.com" {
		t.Errorf("expected email admin@example.com, got %s", rec.Email)
	}
	if rec.DisplayName != "Alireza" {
		t.Errorf("expected display name Alireza, got %s", rec.DisplayName)
	}
	if len(rec.RootSigningSecret) != 64 {
		t.Errorf("expected 64-character hex secret, got len %d", len(rec.RootSigningSecret))
	}
}

func TestAdminInit_APIServer_InteractiveCode(t *testing.T) {
	ts := newMockAdminServer(t, "admin@example.com", "883311")
	defer ts.Close()

	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	in := bytes.NewBufferString("883311\n")

	cmd := newRootCmd()
	cmd.SetIn(in)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "admin@example.com",
		"--server", ts.URL,
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init with interactive code failed: %v", err)
	}

	store := owner.NewStore(storePath)
	if !store.IsVerified() {
		t.Fatalf("expected store to be verified")
	}
}

func TestAdminInit_APIServer_ChallengeFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "mail relay connection timeout"})
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "admin@example.com",
		"--server", ts.URL,
		"--store", storePath,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected admin init to fail when challenge endpoint returns 500")
	}
	if !strings.Contains(err.Error(), "failed to initiate challenge on server") {
		t.Errorf("expected challenge initiation error, got: %v", err)
	}
}

func TestAdminInit_APIServer_InvalidVerificationCode(t *testing.T) {
	ts := newMockAdminServer(t, "admin@example.com", "123456")
	defer ts.Close()

	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "admin@example.com",
		"--code", "999999", // wrong code
		"--server", ts.URL,
		"--store", storePath,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected admin init to fail with invalid verification code")
	}
	if !strings.Contains(err.Error(), "remote OTP verification failed") && !strings.Contains(err.Error(), "rejected") {
		t.Errorf("expected remote OTP verification failure, got: %v", err)
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
		"--owner", "admin@example.com",
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
	if !strings.Contains(out, "admin@example.com") {
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
	if rec.Email != "admin@example.com" {
		t.Errorf("expected email admin@example.com, got %s", rec.Email)
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
		Email:             "admin@example.com",
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
		"--owner", "admin@example.com",
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
		"--owner", "admin@example.com",
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
	if loaded.Email != "admin@example.com" {
		t.Errorf("expected updated email admin@example.com, got %s", loaded.Email)
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

func TestAdminInit_APIServer_EnvOverride(t *testing.T) {
	ts := newMockAdminServer(t, "admin@example.com", "748291")
	defer ts.Close()

	t.Setenv("ORBIT_SERVER", ts.URL)

	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "init",
		"--owner", "admin@example.com",
		"--code", "748291",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin init with ORBIT_SERVER env failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Challenge accepted for admin@example.com") {
		t.Errorf("output missing dispatch confirmation: %s", out)
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
	if !strings.Contains(out, "orbit admin init --owner") {
		t.Errorf("expected suggestion to run init: %s", out)
	}
}

func TestAdminStatus_VerifiedTable(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	rec := &owner.OwnerRecord{
		Email:             "admin@example.com",
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
	if !strings.Contains(out, "admin@example.com") {
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
		Email:             "admin@example.com",
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
	if res.Email != "admin@example.com" {
		t.Errorf("expected email admin@example.com, got %s", res.Email)
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
		Email:             "admin@example.com",
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
		Email:             "admin@example.com",
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

func TestAdminGrant_Success(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	origSecret, _ := owner.GenerateMasterSecret()
	rec := &owner.OwnerRecord{
		Email:             "admin@manova.space",
		DisplayName:       "Owner",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: origSecret,
		KeyFingerprint:    owner.ComputeFingerprint(origSecret),
	}
	_ = store.SaveOwner(rec)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "grant", "sara@manova.space",
		"--store", storePath,
		"--role", "admin",
		"--ttl", "30m",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin grant failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Administrator Grant Generated") {
		t.Errorf("output missing grant header: %s", out)
	}
	if !strings.Contains(out, "sara@manova.space") {
		t.Errorf("output missing recipient email: %s", out)
	}
	if !strings.Contains(out, "Grant Code:") {
		t.Errorf("output missing Grant Code: %s", out)
	}
}

func TestAdminGrant_UnverifiedError(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "grant", "sara@manova.space",
		"--store", storePath,
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when running grant unverified, got nil")
	}
}

func TestAdminGrant_JSONOutput(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	origSecret, _ := owner.GenerateMasterSecret()
	rec := &owner.OwnerRecord{
		Email:             "admin@manova.space",
		DisplayName:       "Owner",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: origSecret,
	}
	_ = store.SaveOwner(rec)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "grant", "sara@manova.space",
		"--store", storePath,
		"--json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin grant --json failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("failed to parse JSON output: %v, raw: %s", err, buf.String())
	}
	if data["email"] != "sara@manova.space" {
		t.Errorf("expected email sara@manova.space, got %v", data["email"])
	}
	if data["code"] == nil || !strings.Contains(data["code"].(string), "-") {
		t.Errorf("expected 8-digit formatted code, got %v", data["code"])
	}
}

func TestAdminTOTPReset_Success(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "owner.json")

	store := owner.NewStore(storePath)
	origSecret, _ := owner.GenerateMasterSecret()
	rec := &owner.OwnerRecord{
		Email:             "admin@manova.space",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: origSecret,
	}
	_ = store.SaveOwner(rec)

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"admin", "totp", "reset", "user@manova.space",
		"--store", storePath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("admin totp reset failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "User TOTP Reset & Recovery Issued") {
		t.Errorf("output missing reset header: %s", out)
	}
	if !strings.Contains(out, "user@manova.space") {
		t.Errorf("output missing recipient email: %s", out)
	}
}

