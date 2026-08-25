package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/worker"
)

func TestWorkerHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"worker", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("worker --help failed: %v", err)
	}

	output := buf.String()
	for _, sub := range []string{"start", "stop", "status", "run-once", "run"} {
		if !strings.Contains(output, sub) {
			t.Errorf("expected worker help to list subcommand %q, got:\n%s", sub, output)
		}
	}
}

func TestWorkerStartAndStopHelp(t *testing.T) {
	// Test start --help
	bufStart := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(bufStart)
	rootCmd.SetErr(bufStart)
	rootCmd.SetArgs([]string{"worker", "start", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("worker start --help failed: %v", err)
	}
	if !strings.Contains(bufStart.String(), "--exec") {
		t.Errorf("expected start help to document --exec flag")
	}

	// Test stop --help
	bufStop := new(bytes.Buffer)
	rootCmd = newRootCmd()
	rootCmd.SetOut(bufStop)
	rootCmd.SetErr(bufStop)
	rootCmd.SetArgs([]string{"worker", "stop", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("worker stop --help failed: %v", err)
	}
	if !strings.Contains(bufStop.String(), "Stop the background worker daemon") {
		t.Errorf("expected stop help to document command")
	}
}

func TestWorkerRunOnceCmd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := worker.EdgeVersionResponse{
			Version:    "v0.1.9",
			Highlights: []string{"Feature A", "Feature B"},
			Status:     "operational",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "edge-version.json")

	// 1. Test standard output
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"worker", "run-once", "--endpoint", server.URL, "--state", stateFile})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("worker run-once failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Edge feed check completed successfully") && !strings.Contains(output, "Edge version check completed successfully") {
		t.Errorf("expected success message, got: %s", output)
	}
	if !strings.Contains(output, "v0.1.9") {
		t.Errorf("expected output to contain v0.1.9, got: %s", output)
	}

	// 2. Test JSON output
	jsonBuf := new(bytes.Buffer)
	rootCmd = newRootCmd()
	rootCmd.SetOut(jsonBuf)
	rootCmd.SetErr(jsonBuf)
	rootCmd.SetArgs([]string{"worker", "run-once", "--endpoint", server.URL, "--state", stateFile, "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("worker run-once --json failed: %v", err)
	}

	var jsonState worker.EdgeVersionState
	if err := json.Unmarshal(jsonBuf.Bytes(), &jsonState); err != nil {
		t.Fatalf("failed to parse json output: %v, raw:\n%s", err, jsonBuf.String())
	}
	if jsonState.LatestVersion != "v0.1.9" {
		t.Errorf("jsonState.LatestVersion = %q, want v0.1.9", jsonState.LatestVersion)
	}
	if jsonState.ServerStatus != "ok" {
		t.Errorf("jsonState.ServerStatus = %q, want ok", jsonState.ServerStatus)
	}
}

func TestWorkerStatusCmd(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

	stateFile := filepath.Join(tempDir, "edge-version.json")
	_ = worker.WriteStateAtomic(stateFile, &worker.EdgeVersionState{
		LatestVersion: "v0.1.9",
		ServerStatus:  "ok",
		LastCheckedAt: time.Now().UTC(),
	})

	// 1. Test status formatted card
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"worker", "status", "--state", stateFile})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("worker status failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Manova Background Worker Status") {
		t.Errorf("expected status header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "v0.1.9") {
		t.Errorf("expected v0.1.9 in output, got:\n%s", output)
	}

	// 2. Test status JSON output
	jsonBuf := new(bytes.Buffer)
	rootCmd = newRootCmd()
	rootCmd.SetOut(jsonBuf)
	rootCmd.SetErr(jsonBuf)
	rootCmd.SetArgs([]string{"worker", "status", "--state", stateFile, "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("worker status --json failed: %v", err)
	}

	var status worker.DaemonStatus
	if err := json.Unmarshal(jsonBuf.Bytes(), &status); err != nil {
		t.Fatalf("failed to unmarshal status JSON: %v, raw:\n%s", err, jsonBuf.String())
	}
	if status.LatestVersion != "v0.1.9" {
		t.Errorf("status.LatestVersion = %q, want v0.1.9", status.LatestVersion)
	}
}
