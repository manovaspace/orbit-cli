package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestServerCmd_Flags(t *testing.T) {
	cmd := newRootCmd()
	if cmd == nil {
		t.Fatal("expected newRootCmd() to return non-nil command")
	}
	if cmd.Use != "orbit-server" {
		t.Errorf("expected cmd.Use to be 'orbit-server', got %q", cmd.Use)
	}

	requiredFlags := []struct {
		name     string
		defValue string
	}{
		{"addr", ":8080"},
		{"smtp-host", ""},
		{"smtp-port", ""},
		{"smtp-user", ""},
		{"smtp-pass", ""},
		{"smtp-from", ""},
		{"signing-secret", ""},
		{"config", ""},
		{"store", ""},
		{"owner-store", ""},
		{"db", ""},
		{"version", "false"},
	}

	for _, rf := range requiredFlags {
		flag := cmd.Flags().Lookup(rf.name)
		if flag == nil {
			t.Errorf("expected flag --%s to exist", rf.name)
			continue
		}
		if rf.defValue != "" && flag.DefValue != rf.defValue {
			t.Errorf("expected default value for --%s to be %q, got %q", rf.name, rf.defValue, flag.DefValue)
		}
	}
}

func TestServerCmd_FlagParsing(t *testing.T) {
	cmd := newRootCmd()
	args := []string{
		"--addr", "127.0.0.1:9099",
		"--smtp-host", "smtp.custom.org",
		"--smtp-port", "2525",
		"--smtp-user", "customuser",
		"--smtp-pass", "custompass",
		"--smtp-from", "noreply@custom.org",
		"--signing-secret", "supersecret123456789012345678901234",
		"--config", "/etc/orbit/server.yaml",
		"--store", "/var/lib/orbit/invites.json",
		"--owner-store", "/var/lib/orbit/owner.json",
		"--db", "/var/lib/orbit/orbit.db",
		"--trusted-proxies", "127.0.0.1/32,10.0.0.0/8",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	if addr, _ := cmd.Flags().GetString("addr"); addr != "127.0.0.1:9099" {
		t.Errorf("expected addr '127.0.0.1:9099', got %q", addr)
	}
	if host, _ := cmd.Flags().GetString("smtp-host"); host != "smtp.custom.org" {
		t.Errorf("expected smtp-host 'smtp.custom.org', got %q", host)
	}
	if port, _ := cmd.Flags().GetString("smtp-port"); port != "2525" {
		t.Errorf("expected smtp-port '2525', got %q", port)
	}
	if user, _ := cmd.Flags().GetString("smtp-user"); user != "customuser" {
		t.Errorf("expected smtp-user 'customuser', got %q", user)
	}
	if pass, _ := cmd.Flags().GetString("smtp-pass"); pass != "custompass" {
		t.Errorf("expected smtp-pass 'custompass', got %q", pass)
	}
	if from, _ := cmd.Flags().GetString("smtp-from"); from != "noreply@custom.org" {
		t.Errorf("expected smtp-from 'noreply@custom.org', got %q", from)
	}
	if secret, _ := cmd.Flags().GetString("signing-secret"); secret != "supersecret123456789012345678901234" {
		t.Errorf("expected signing-secret 'supersecret123456789012345678901234', got %q", secret)
	}
	if cfg, _ := cmd.Flags().GetString("config"); cfg != "/etc/orbit/server.yaml" {
		t.Errorf("expected config '/etc/orbit/server.yaml', got %q", cfg)
	}
	if store, _ := cmd.Flags().GetString("store"); store != "/var/lib/orbit/invites.json" {
		t.Errorf("expected store '/var/lib/orbit/invites.json', got %q", store)
	}
	if ownerStore, _ := cmd.Flags().GetString("owner-store"); ownerStore != "/var/lib/orbit/owner.json" {
		t.Errorf("expected owner-store '/var/lib/orbit/owner.json', got %q", ownerStore)
	}
	if db, _ := cmd.Flags().GetString("db"); db != "/var/lib/orbit/orbit.db" {
		t.Errorf("expected db '/var/lib/orbit/orbit.db', got %q", db)
	}
	proxies, _ := cmd.Flags().GetStringSlice("trusted-proxies")
	if len(proxies) != 2 || proxies[0] != "127.0.0.1/32" || proxies[1] != "10.0.0.0/8" {
		t.Errorf("expected trusted-proxies [127.0.0.1/32 10.0.0.0/8], got %v", proxies)
	}
}

func TestDefaultDBPath(t *testing.T) {
	// Case 1: ORBIT_DB_PATH environment variable override
	t.Run("EnvOverride", func(t *testing.T) {
		t.Setenv("ORBIT_DB_PATH", "/custom/path/orbit.db")
		p := DefaultDBPath()
		if p != "/custom/path/orbit.db" {
			t.Errorf("expected /custom/path/orbit.db, got %s", p)
		}
	})

	// Case 2: Default user config directory
	t.Run("DefaultHome", func(t *testing.T) {
		t.Setenv("ORBIT_DB_PATH", "")
		p := DefaultDBPath()
		if p == "" {
			t.Fatal("expected non-empty default db path")
		}
		if !strings.HasSuffix(p, "orbit.db") {
			t.Errorf("expected path to end with orbit.db, got %s", p)
		}
	})
}

func TestServerCmd_Version(t *testing.T) {
	// 1. Subcommand: orbit-server version
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error running 'version', got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "orbit-server version") {
		t.Errorf("expected version output to contain 'orbit-server version', got %q", out)
	}

	// 2. Flag: orbit-server --version
	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error running '--version', got: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "orbit-server version") {
		t.Errorf("expected --version output to contain 'orbit-server version', got %q", out)
	}
}

func getFreeLocalAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free local address: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestServerCmd_HealthAndGracefulShutdown(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "orbit.db")
	addr := getFreeLocalAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{
		"--addr", addr,
		"--db", dbPath,
		"--signing-secret", "testsecret123456789012345678901234",
	})

	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.Execute()
	}()

	baseURL := fmt.Sprintf("http://%s", addr)
	healthzURL := baseURL + "/healthz"
	onboardHealthURL := baseURL + "/v1/onboard/health"
	healthURL := baseURL + "/health"

	var resp *http.Response
	var err error
	deadline := time.Now().Add(5 * time.Second)
	client := &http.Client{Timeout: 500 * time.Millisecond}

	// Wait for /healthz probe to respond
	for time.Now().Before(deadline) {
		resp, err = client.Get(healthzURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("server did not respond on /healthz: err=%v, resp=%v", err, resp)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var healthData map[string]interface{}
	if err := json.Unmarshal(body, &healthData); err != nil {
		t.Fatalf("failed to parse /healthz JSON: %v, body: %s", err, string(body))
	}
	if healthData["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", healthData["status"])
	}

	// Test /v1/onboard/health endpoint
	resp2, err := client.Get(onboardHealthURL)
	if err != nil || resp2.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET %s: %v", onboardHealthURL, err)
	}
	_ = resp2.Body.Close()

	// Test /health endpoint
	resp3, err := client.Get(healthURL)
	if err != nil || resp3.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET %s: %v", healthURL, err)
	}
	_ = resp3.Body.Close()

	// Trigger graceful shutdown
	cancel()

	select {
	case execErr := <-errChan:
		if execErr != nil {
			t.Fatalf("unexpected error during graceful shutdown: %v", execErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for orbit-server to shut down gracefully")
	}

	out := buf.String()
	if !strings.Contains(out, addr) {
		t.Errorf("expected output to mention listen address %s, got: %s", addr, out)
	}
	if !strings.Contains(out, "Server shutdown complete") && !strings.Contains(out, "Gracefully shutting down") {
		t.Errorf("expected output to mention shutdown, got: %s", out)
	}
}

func TestResolveSigningSecret(t *testing.T) {
	// Case 1: Flag secret takes highest precedence
	sec, src := resolveSigningSecret("my-flag-secret", "")
	if sec != "my-flag-secret" || !strings.Contains(src, "flag") {
		t.Errorf("expected flag secret, got %s (%s)", sec, src)
	}

	// Case 2: Env vars (ORBIT_SIGNING_SECRET, ORBIT_INVITE_SECRET, ORBIT_JWT_SECRET)
	for _, envKey := range []string{"ORBIT_SIGNING_SECRET", "ORBIT_INVITE_SECRET", "ORBIT_JWT_SECRET"} {
		t.Run(envKey, func(t *testing.T) {
			t.Setenv(envKey, "env-secret-123456789012345678901234")
			s, srcInfo := resolveSigningSecret("", "")
			if s != "env-secret-123456789012345678901234" || !strings.Contains(srcInfo, envKey) {
				t.Errorf("expected %s secret, got %s (%s)", envKey, s, srcInfo)
			}
		})
	}

	// Case 3: Legacy MANOVA_* env vars are ignored
	for _, legacyEnv := range []string{"MANOVA_INVITE_SECRET", "MANOVA_JWT_SECRET"} {
		t.Run("ignore_"+legacyEnv, func(t *testing.T) {
			t.Setenv(legacyEnv, "legacy-secret-12345678901234567890")
			s, srcInfo := resolveSigningSecret("", "")
			if s == "legacy-secret-12345678901234567890" || strings.Contains(srcInfo, legacyEnv) {
				t.Errorf("expected %s to be ignored, got %s (%s)", legacyEnv, s, srcInfo)
			}
		})
	}

	// Case 4: Owner store
	tmpDir := t.TempDir()
	ownerPath := filepath.Join(tmpDir, "owner.json")
	store := owner.NewStore(ownerPath)
	err := store.SaveOwner(&owner.OwnerRecord{
		Email:             "admin@manova.space",
		RootSigningSecret: "owner-secret-12345678901234567890",
		VerifiedAt:        time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save owner record: %v", err)
	}

	sec, src = resolveSigningSecret("", ownerPath)
	if sec != "owner-secret-12345678901234567890" || !strings.Contains(src, "admin@manova.space") {
		t.Errorf("expected owner vault secret, got %s (%s)", sec, src)
	}

	// Case 5: Fallback
	sec, src = resolveSigningSecret("", filepath.Join(tmpDir, "nonexistent.json"))
	if sec != DefaultFallbackSecret || !strings.Contains(src, "fallback") {
		t.Errorf("expected fallback secret, got %s (%s)", sec, src)
	}
}

func TestServer_E2E_FullLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "orbit.db")
	legacyStorePath := filepath.Join(tempDir, "legacy_invites.json")

	secret := "test-secret-key-32bytes-long-12345"
	secretBytes := []byte(secret)

	// 1. Create a legacy invites JSON file with an invite
	tok1, claims1, err := invite.GenerateToken(invite.InviteRequest{
		Email:       "alice@manova.space",
		DisplayName: "Alice",
		Scope:       "orbit",
		TTL:         24 * time.Hour,
	}, secretBytes)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	legacyInvites := []*invite.InviteRecord{
		{
			ID:        claims1.ID,
			Email:     claims1.Email,
			Token:     tok1,
			Scope:     claims1.Scope,
			ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		},
	}
	legacyData, _ := json.Marshal(legacyInvites)
	if err := os.WriteFile(legacyStorePath, legacyData, 0600); err != nil {
		t.Fatalf("failed to write legacy invites: %v", err)
	}

	addr := getFreeLocalAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{
		"--addr", addr,
		"--db", dbPath,
		"--store", legacyStorePath,
		"--signing-secret", secret,
		"--trusted-proxies", "127.0.0.1/32,10.0.0.0/8",
	})

	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.Execute()
	}()

	baseURL := fmt.Sprintf("http://%s", addr)
	httpClient := &http.Client{Timeout: 2 * time.Second}

	// 2. Wait for server to be healthy
	deadline := time.Now().Add(5 * time.Second)
	healthy := false
	for time.Now().Before(deadline) {
		resp, err := httpClient.Get(baseURL + "/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			healthy = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !healthy {
		t.Fatal("server failed to start and respond to /healthz")
	}

	// 3. Verify SQLite DB was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected SQLite DB file at %s", dbPath)
	}

	// Verify legacy JSON was migrated and renamed to .bak
	if _, err := os.Stat(legacyStorePath + ".bak"); os.IsNotExist(err) {
		t.Errorf("expected legacy store to be renamed to %s.bak", legacyStorePath)
	}

	// Open DB connection to inspect migrations & schema
	db, err := sqlite.NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db directly: %v", err)
	}
	defer db.Close()

	// Verify migrated invite is queryable in SQLite
	migratedRec, err := db.Invites().GetInvite(context.Background(), claims1.ID)
	if err != nil || migratedRec == nil {
		t.Fatalf("expected migrated invite in DB: %v, rec: %+v", err, migratedRec)
	}
	if migratedRec.Email != "alice@manova.space" {
		t.Errorf("expected migrated email alice@manova.space, got %s", migratedRec.Email)
	}

	// 4. Test HTTP claim with migrated token
	claimBody, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken:        tok1,
		DesiredUID:         "alice",
		SSHPublicKey:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICustomAlicePublicKey alice@device",
		MachineFingerprint: "alice-machine-1",
	})
	req, _ := http.NewRequest("POST", baseURL+"/v1/onboard/claim", bytes.NewReader(claimBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idemp-alice-e2e")

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("claim failed: code %d, body: %s", resp.StatusCode, string(body))
	}
	var claimResp provisioner.ClaimResponse
	_ = json.NewDecoder(resp.Body).Decode(&claimResp)
	_ = resp.Body.Close()

	if claimResp.Status != "success" || claimResp.User.UID != "alice" {
		t.Errorf("unexpected claim response: %+v", claimResp)
	}

	// Test claim idempotency replay
	reqReplay, _ := http.NewRequest("POST", baseURL+"/v1/onboard/claim", bytes.NewReader(claimBody))
	reqReplay.Header.Set("Content-Type", "application/json")
	reqReplay.Header.Set("Idempotency-Key", "idemp-alice-e2e")
	respReplay, err := httpClient.Do(reqReplay)
	if err != nil || respReplay.StatusCode != http.StatusOK {
		t.Fatalf("replay failed: status %d", respReplay.StatusCode)
	}
	var claimRespReplay provisioner.ClaimResponse
	_ = json.NewDecoder(respReplay.Body).Decode(&claimRespReplay)
	_ = respReplay.Body.Close()
	if !claimRespReplay.IdempotentReplay {
		t.Errorf("expected IdempotentReplay == true on repeated claim")
	}

	// 5. Test Revocation Enforcement via SQLite Store
	revokedToken, revokedClaims, err := invite.GenerateToken(invite.InviteRequest{
		Email: "revoked@manova.space",
		TTL:   24 * time.Hour,
	}, secretBytes)
	if err != nil {
		t.Fatalf("failed to generate revoked token: %v", err)
	}
	err = db.Invites().SaveInvite(context.Background(), &invite.InviteRecord{
		ID:        revokedClaims.ID,
		Email:     "revoked@manova.space",
		Token:     revokedToken,
		Revoked:   true,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to save revoked invite: %v", err)
	}

	claimRevBody, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken: revokedToken,
		DesiredUID:  "revokeduser",
	})
	reqRev, _ := http.NewRequest("POST", baseURL+"/v1/onboard/claim", bytes.NewReader(claimRevBody))
	reqRev.Header.Set("Content-Type", "application/json")
	respRev, err := httpClient.Do(reqRev)
	if err != nil {
		t.Fatalf("revoked claim request failed: %v", err)
	}
	if respRev.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for revoked invite, got %d", respRev.StatusCode)
	}
	_ = respRev.Body.Close()

	// 6. Test Admin Grant Creation and Verification with SQLite persistence
	grantBody, _ := json.Marshal(map[string]interface{}{
		"email":       "admin@manova.space",
		"role":        "admin",
		"ttl_seconds": 600,
	})
	grantReq, _ := http.NewRequest("POST", baseURL+"/api/v1/admin/grants", bytes.NewReader(grantBody))
	grantReq.Header.Set("Content-Type", "application/json")
	grantReq.Header.Set("Authorization", "Bearer "+secret)

	grantResp, err := httpClient.Do(grantReq)
	if err != nil || grantResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(grantResp.Body)
		t.Fatalf("admin grant create failed: code %d, body: %s", grantResp.StatusCode, string(body))
	}
	var grantData map[string]interface{}
	_ = json.NewDecoder(grantResp.Body).Decode(&grantData)
	_ = grantResp.Body.Close()

	codeFormatted, ok := grantData["code"].(string)
	if !ok || codeFormatted == "" {
		t.Fatalf("expected non-empty grant code in response: %+v", grantData)
	}

	// Verify grant via /api/v1/admin/verify
	verifyBody, _ := json.Marshal(map[string]string{
		"email":        "admin@manova.space",
		"code":         codeFormatted,
		"display_name": "Admin User",
	})
	vReq, _ := http.NewRequest("POST", baseURL+"/api/v1/admin/verify", bytes.NewReader(verifyBody))
	vReq.Header.Set("Content-Type", "application/json")
	vResp, err := httpClient.Do(vReq)
	if err != nil || vResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(vResp.Body)
		t.Fatalf("admin verify failed: code %d, body: %s", vResp.StatusCode, string(body))
	}
	var vData map[string]interface{}
	_ = json.NewDecoder(vResp.Body).Decode(&vData)
	_ = vResp.Body.Close()
	if vData["status"] != "verified" {
		t.Errorf("expected verify status 'verified', got %v", vData["status"])
	}

	// Verify grant is now marked used in SQLite
	grants, err := db.Grants().ListActiveGrants(context.Background())
	if err != nil {
		t.Fatalf("failed to list active grants: %v", err)
	}
	for _, g := range grants {
		if g.Email == "admin@manova.space" {
			t.Errorf("expected grant for admin@manova.space to be marked used, but still active")
		}
	}

	// 7. Test Challenge creation and verification via SQLite
	chMgr := owner.NewPersistentChallengeManager(db.Challenges())
	ch, otp, err := chMgr.CreateChallenge("operator@manova.space", 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to create challenge: %v", err)
	}
	if ch.ID == "" {
		t.Fatalf("expected non-empty challenge ID")
	}

	chVerifyBody, _ := json.Marshal(map[string]string{
		"email": "operator@manova.space",
		"code":  otp,
	})
	chVReq, _ := http.NewRequest("POST", baseURL+"/api/v1/admin/verify", bytes.NewReader(chVerifyBody))
	chVReq.Header.Set("Content-Type", "application/json")
	chVResp, err := httpClient.Do(chVReq)
	if err != nil || chVResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(chVResp.Body)
		t.Fatalf("challenge verify failed: code %d, body: %s", chVResp.StatusCode, string(body))
	}
	_ = chVResp.Body.Close()

	// 8. Verify Rate Limit Events recorded in SQLite
	for i := 0; i < 3; i++ {
		r, _ := httpClient.Get(baseURL + "/healthz")
		if r != nil {
			_ = r.Body.Close()
		}
	}
	count, err := db.RateLimits().CountEventsSince(context.Background(), "127.0.0.1", "/health", time.Now().UTC().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("failed to count rate limit events: %v", err)
	}
	if count == 0 {
		t.Errorf("expected rate limit events to be recorded in SQLite, got 0")
	}

	// 9. Graceful shutdown
	cancel()
	select {
	case execErr := <-errChan:
		if execErr != nil {
			t.Fatalf("unexpected error on shutdown: %v", execErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server shutdown")
	}

	outStr := buf.String()
	if !strings.Contains(outStr, dbPath) {
		t.Errorf("expected server output to mention db path %s, got: %s", dbPath, outStr)
	}
}

