package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestManovaServeCmd_Init(t *testing.T) {
	cmd := newServeCmd()
	if cmd == nil {
		t.Fatal("expected newServeCmd() to return non-nil command")
	}
	if cmd.Use != "serve" {
		t.Errorf("expected cmd.Use to be 'serve', got %q", cmd.Use)
	}

	// Verify required flags exist
	requiredFlags := []string{
		"addr",
		"smtp-host",
		"smtp-port",
		"smtp-user",
		"smtp-pass",
		"smtp-from",
		"signing-secret",
	}

	for _, flagName := range requiredFlags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected flag --%s to exist", flagName)
		}
	}

	addrFlag := cmd.Flags().Lookup("addr")
	if addrFlag == nil || addrFlag.DefValue != ":8080" {
		t.Errorf("expected default --addr to be ':8080', got %v", addrFlag)
	}
}

func TestManovaServeCmd_FlagParsing(t *testing.T) {
	cmd := newServeCmd()
	args := []string{
		"--addr", "127.0.0.1:9098",
		"--smtp-host", "smtp.manova.org",
		"--smtp-port", "587",
		"--smtp-user", "manovauser",
		"--smtp-pass", "manovapass",
		"--smtp-from", "noreply@manova.org",
		"--signing-secret", "manovasecret123456789012345678901234",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	if addr, _ := cmd.Flags().GetString("addr"); addr != "127.0.0.1:9098" {
		t.Errorf("expected addr '127.0.0.1:9098', got %q", addr)
	}
	if host, _ := cmd.Flags().GetString("smtp-host"); host != "smtp.manova.org" {
		t.Errorf("expected smtp-host 'smtp.manova.org', got %q", host)
	}
	if port, _ := cmd.Flags().GetString("smtp-port"); port != "587" {
		t.Errorf("expected smtp-port '587', got %q", port)
	}
	if user, _ := cmd.Flags().GetString("smtp-user"); user != "manovauser" {
		t.Errorf("expected smtp-user 'manovauser', got %q", user)
	}
	if pass, _ := cmd.Flags().GetString("smtp-pass"); pass != "manovapass" {
		t.Errorf("expected smtp-pass 'manovapass', got %q", pass)
	}
	if from, _ := cmd.Flags().GetString("smtp-from"); from != "noreply@manova.org" {
		t.Errorf("expected smtp-from 'noreply@manova.org', got %q", from)
	}
	if secret, _ := cmd.Flags().GetString("signing-secret"); secret != "manovasecret123456789012345678901234" {
		t.Errorf("expected signing-secret 'manovasecret123456789012345678901234', got %q", secret)
	}
}

func getFreeLocalAddrManova(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free local address: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestManovaServeCmd_HealthAndGracefulShutdown(t *testing.T) {
	addr := getFreeLocalAddrManova(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{
		"serve",
		"--addr", addr,
		"--signing-secret", "manovatestsecret12345678901234567890",
	})

	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.Execute()
	}()

	baseURL := fmt.Sprintf("http://%s", addr)
	healthURL := baseURL + "/health"
	onboardHealthURL := baseURL + "/v1/onboard/health"

	var resp *http.Response
	var err error
	deadline := time.Now().Add(5 * time.Second)
	client := &http.Client{Timeout: 500 * time.Millisecond}

	for time.Now().Before(deadline) {
		resp, err = client.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("server did not respond on /health: err=%v, resp=%v", err, resp)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var healthData map[string]interface{}
	if err := json.Unmarshal(body, &healthData); err != nil {
		t.Fatalf("failed to parse health JSON: %v, body: %s", err, string(body))
	}
	if healthData["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", healthData["status"])
	}

	// Test /v1/onboard/health
	resp2, err := client.Get(onboardHealthURL)
	if err != nil || resp2.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET %s: %v", onboardHealthURL, err)
	}
	_ = resp2.Body.Close()

	// Trigger graceful shutdown
	cancel()

	select {
	case execErr := <-errChan:
		if execErr != nil {
			t.Fatalf("unexpected error from serve command during graceful shutdown: %v", execErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server to shutdown gracefully")
	}

	out := buf.String()
	if !strings.Contains(out, "listening on") && !strings.Contains(out, addr) {
		t.Errorf("expected output to mention listening address %s, got: %s", addr, out)
	}
}
