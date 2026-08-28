package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner"
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
