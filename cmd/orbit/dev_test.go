package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDevCmd_Registration(t *testing.T) {
	cmd := newDevCmd()
	if cmd.Use != "dev" {
		t.Errorf("expected command use 'dev', got %q", cmd.Use)
	}

	expectedSubcommands := []string{"up", "down", "tier2", "caddy", "portal", "logs"}
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}

	for _, expected := range expectedSubcommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand %q to be registered under 'dev'", expected)
		}
	}
}

func TestDevUpCmd_Flags(t *testing.T) {
	cmd := newDevUpCmd()

	waitFlag := cmd.Flags().Lookup("wait")
	if waitFlag == nil {
		t.Fatal("expected --wait flag on dev up")
	}
	if waitFlag.DefValue != "false" {
		t.Errorf("expected --wait default to be false, got %s", waitFlag.DefValue)
	}
	if waitFlag.Shorthand != "w" {
		t.Errorf("expected --wait shorthand to be 'w', got %q", waitFlag.Shorthand)
	}

	timeoutFlag := cmd.Flags().Lookup("timeout")
	if timeoutFlag == nil {
		t.Fatal("expected --timeout flag on dev up")
	}
	if timeoutFlag.DefValue != "15s" {
		t.Errorf("expected --timeout default to be 15s, got %s", timeoutFlag.DefValue)
	}
}

func TestCheckPortReady(t *testing.T) {
	// 1. Live open port should return true
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port
	if !checkPortReady("127.0.0.1", port, 500*time.Millisecond) {
		t.Errorf("expected checkPortReady to return true for open port %d", port)
	}

	// 2. Closed port should return false
	_ = l.Close()
	// Allow OS socket cleanup
	time.Sleep(20 * time.Millisecond)
	if checkPortReady("127.0.0.1", port, 50*time.Millisecond) {
		t.Errorf("expected checkPortReady to return false for closed port %d", port)
	}
}

func TestWaitForServiceHealth_Success(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port
	ctx := context.Background()

	if !waitForServiceHealth(ctx, "127.0.0.1", port, 1*time.Second) {
		t.Errorf("expected waitForServiceHealth to return true for open port %d", port)
	}
}

func TestWaitForServiceHealth_Timeout(t *testing.T) {
	// Use an unused port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	ctx := context.Background()
	start := time.Now()
	res := waitForServiceHealth(ctx, "127.0.0.1", port, 100*time.Millisecond)
	elapsed := time.Since(start)

	if res {
		t.Errorf("expected waitForServiceHealth to return false for closed port %d", port)
	}
	if elapsed < 80*time.Millisecond {
		t.Errorf("expected waitForServiceHealth to respect timeout, elapsed: %v", elapsed)
	}
}

func TestWaitForServiceHealth_ContextCancel(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	res := waitForServiceHealth(ctx, "127.0.0.1", port, 2*time.Second)
	if res {
		t.Errorf("expected waitForServiceHealth to return false for cancelled context")
	}
}

func TestProbeServiceReadiness(t *testing.T) {
	ctx := context.Background()
	statuses := probeServiceReadiness(ctx, 100*time.Millisecond)

	if len(statuses) != 4 {
		t.Fatalf("expected 4 service statuses, got %d", len(statuses))
	}

	expectedServices := map[string]int{
		"Postgres":      10332,
		"NATS":          10482,
		"LLDAP":         10389,
		"Caddy Ingress": 10000,
	}

	for _, s := range statuses {
		expPort, ok := expectedServices[s.Name]
		if !ok {
			t.Errorf("unexpected service in probe results: %s", s.Name)
			continue
		}
		if s.Port != expPort {
			t.Errorf("service %s expected port %d, got %d", s.Name, expPort, s.Port)
		}
		if s.Message == "" {
			t.Errorf("service %s had empty message", s.Name)
		}
	}
}

func TestDevPortalCmd_Output(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := newDevPortalCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("portal command failed: %v", err)
	}

	out := buf.String()
	expectedStrings := []string{
		"Orbit Local Developer Portal",
		"http://localhost:10007",
		"http://auth.dev.manova.space:10000",
		"http://git.dev.manova.space:10000",
		"http://mail.dev.manova.space:10000",
		"http://grafana.dev.manova.space:10000",
	}

	for _, exp := range expectedStrings {
		if !strings.Contains(out, exp) {
			t.Errorf("expected portal output to contain %q, got:\n%s", exp, out)
		}
	}
}

func setupMockInfra(t *testing.T) (string, *[]string) {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("ORBIT_INFRA_DIR", tempDir)

	var recordedCalls []string
	origRunner := infraRunner
	infraRunner = func(dir string, stdout, stderr io.Writer, cmdName string, args ...string) error {
		call := fmt.Sprintf("%s %s", cmdName, strings.Join(args, " "))
		recordedCalls = append(recordedCalls, call)
		return nil
	}

	t.Cleanup(func() {
		infraRunner = origRunner
	})

	return tempDir, &recordedCalls
}

func TestDevUpCmd_Execution(t *testing.T) {
	tempDir, recordedCalls := setupMockInfra(t)
	// Create mock compose.sh
	_ = os.WriteFile(filepath.Join(tempDir, "compose.sh"), []byte("#!/bin/sh\n"), 0755)

	buf := new(bytes.Buffer)
	cmd := newDevUpCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--timeout", "100ms"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("dev up failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Starting Orbit Dev Stack") {
		t.Errorf("expected output to contain starting message, got:\n%s", out)
	}
	if !strings.Contains(out, "Orbit Local Developer Endpoints") {
		t.Errorf("expected output to contain endpoints banner, got:\n%s", out)
	}

	if len(*recordedCalls) != 1 {
		t.Fatalf("expected 1 infra command execution, got %d", len(*recordedCalls))
	}
	if !strings.Contains((*recordedCalls)[0], "compose.sh up -d") {
		t.Errorf("expected compose.sh up -d call, got: %s", (*recordedCalls)[0])
	}
}

func TestDevUpCmd_WithWaitFlag(t *testing.T) {
	tempDir, _ := setupMockInfra(t)
	_ = os.WriteFile(filepath.Join(tempDir, "compose.sh"), []byte("#!/bin/sh\n"), 0755)

	buf := new(bytes.Buffer)
	cmd := newDevUpCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--wait", "--timeout", "100ms"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("dev up with --wait failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Probing service readiness") {
		t.Errorf("expected output to indicate probing service readiness, got:\n%s", out)
	}
	if !strings.Contains(out, "Service Readiness:") {
		t.Errorf("expected output to contain Service Readiness table, got:\n%s", out)
	}
}

func TestDevDownCmd_Execution(t *testing.T) {
	tempDir, recordedCalls := setupMockInfra(t)
	_ = os.WriteFile(filepath.Join(tempDir, "compose.sh"), []byte("#!/bin/sh\n"), 0755)

	buf := new(bytes.Buffer)
	cmd := newDevDownCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("dev down failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Stopping Orbit Dev Stack") {
		t.Errorf("expected output to contain stopping message, got:\n%s", out)
	}

	if len(*recordedCalls) != 1 {
		t.Fatalf("expected 1 infra command execution, got %d", len(*recordedCalls))
	}
	if !strings.Contains((*recordedCalls)[0], "compose.sh down") {
		t.Errorf("expected compose.sh down call, got: %s", (*recordedCalls)[0])
	}
}

func TestDevTier2Cmd_Execution(t *testing.T) {
	tempDir, recordedCalls := setupMockInfra(t)
	scriptsDir := filepath.Join(tempDir, "scripts")
	_ = os.MkdirAll(scriptsDir, 0755)
	tier2Script := filepath.Join(scriptsDir, "start-tier2.sh")
	_ = os.WriteFile(tier2Script, []byte("#!/bin/sh\n"), 0755)

	buf := new(bytes.Buffer)
	cmd := newDevTier2Cmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("dev tier2 failed: %v", err)
	}

	if len(*recordedCalls) != 1 {
		t.Fatalf("expected 1 infra command execution, got %d", len(*recordedCalls))
	}
	if !strings.Contains((*recordedCalls)[0], "start-tier2.sh") {
		t.Errorf("expected start-tier2.sh call, got: %s", (*recordedCalls)[0])
	}
}

func TestDevTier2Cmd_MissingScript(t *testing.T) {
	_ , _ = setupMockInfra(t)
	// Don't create start-tier2.sh

	buf := new(bytes.Buffer)
	cmd := newDevTier2Cmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when start-tier2.sh is missing")
	}
	if !strings.Contains(err.Error(), "start-tier2.sh script not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDevCaddyCmd_Actions(t *testing.T) {
	tempDir, recordedCalls := setupMockInfra(t)
	_ = os.WriteFile(filepath.Join(tempDir, "compose.sh"), []byte("#!/bin/sh\n"), 0755)

	// Test reload
	buf := new(bytes.Buffer)
	cmd := newDevCaddyCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"reload"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("caddy reload failed: %v", err)
	}

	// Test restart
	buf.Reset()
	cmd = newDevCaddyCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restart"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("caddy restart failed: %v", err)
	}

	// Test logs
	buf.Reset()
	cmd = newDevCaddyCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"logs"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("caddy logs failed: %v", err)
	}

	// Test unknown action
	buf.Reset()
	cmd = newDevCaddyCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"unknown-action"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown caddy action")
	}

	if len(*recordedCalls) != 3 {
		t.Fatalf("expected 3 caddy calls, got %d", len(*recordedCalls))
	}
}

func TestDevLogsCmd_Execution(t *testing.T) {
	_, recordedCalls := setupMockInfra(t)

	buf := new(bytes.Buffer)
	cmd := newDevLogsCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"postgres"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("dev logs failed: %v", err)
	}

	if len(*recordedCalls) != 1 {
		t.Fatalf("expected 1 logs call, got %d", len(*recordedCalls))
	}
	if !strings.Contains((*recordedCalls)[0], "docker compose logs -f postgres") {
		t.Errorf("expected docker compose logs -f postgres, got: %s", (*recordedCalls)[0])
	}
}

func TestFindOrbitInfraDir(t *testing.T) {
	tempDir := t.TempDir()

	// 1. ORBIT_INFRA_DIR set
	t.Setenv("ORBIT_INFRA_DIR", tempDir)
	found := findOrbitInfraDir("/some/workspace")
	if found != tempDir {
		t.Errorf("expected %s, got %s", tempDir, found)
	}

	// 2. Clear env var and test candidates
	t.Setenv("ORBIT_INFRA_DIR", "")
	wsDir := t.TempDir()
	orbitInfraDir := filepath.Join(wsDir, "orbit", "orbit-infra")
	_ = os.MkdirAll(orbitInfraDir, 0755)
	_ = os.WriteFile(filepath.Join(orbitInfraDir, "compose.sh"), []byte("#!/bin/sh\n"), 0755)

	found = findOrbitInfraDir(wsDir)
	if found != orbitInfraDir {
		t.Errorf("expected %s, got %s", orbitInfraDir, found)
	}
}
