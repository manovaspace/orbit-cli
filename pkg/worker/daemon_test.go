package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetServiceUnitContent(t *testing.T) {
	content := GetServiceUnitContent("/usr/local/bin/manova")

	if !strings.Contains(content, "[Unit]") {
		t.Errorf("expected content to contain [Unit], got:\n%s", content)
	}
	if !strings.Contains(content, "[Service]") {
		t.Errorf("expected content to contain [Service], got:\n%s", content)
	}
	if !strings.Contains(content, "[Install]") {
		t.Errorf("expected content to contain [Install], got:\n%s", content)
	}
	if !strings.Contains(content, "Type=oneshot") {
		t.Errorf("expected content to specify Type=oneshot, got:\n%s", content)
	}
	if !strings.Contains(content, "ExecStart=/usr/local/bin/manova worker run-once") {
		t.Errorf("expected content to specify ExecStart=/usr/local/bin/manova worker run-once, got:\n%s", content)
	}

	// Test default execPath when empty
	defaultContent := GetServiceUnitContent("")
	if !strings.Contains(defaultContent, "worker run-once") {
		t.Errorf("expected default content to contain 'worker run-once', got:\n%s", defaultContent)
	}
}

func TestGetTimerUnitContent(t *testing.T) {
	content := GetTimerUnitContent()

	if !strings.Contains(content, "[Unit]") {
		t.Errorf("expected content to contain [Unit], got:\n%s", content)
	}
	if !strings.Contains(content, "[Timer]") {
		t.Errorf("expected content to contain [Timer], got:\n%s", content)
	}
	if !strings.Contains(content, "OnBootSec=30s") {
		t.Errorf("expected content to contain OnBootSec=30s, got:\n%s", content)
	}
	if !strings.Contains(content, "OnUnitActiveSec=1m") {
		t.Errorf("expected content to contain OnUnitActiveSec=1m, got:\n%s", content)
	}
	if !strings.Contains(content, "Unit=manova-worker.service") {
		t.Errorf("expected content to contain Unit=manova-worker.service, got:\n%s", content)
	}
	if !strings.Contains(content, "WantedBy=timers.target") {
		t.Errorf("expected content to contain WantedBy=timers.target, got:\n%s", content)
	}
}

func TestPIDTracking(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	// Read non-existent
	if _, err := ReadPID(pidFile); err == nil {
		t.Errorf("expected error reading non-existent PID file, got nil")
	}

	// Write PID
	pid := 12345
	if err := WritePID(pidFile, pid); err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}

	// Read back PID
	readPid, err := ReadPID(pidFile)
	if err != nil {
		t.Fatalf("ReadPID failed: %v", err)
	}
	if readPid != pid {
		t.Errorf("ReadPID = %d, want %d", readPid, pid)
	}

	// Remove PID
	if err := RemovePID(pidFile); err != nil {
		t.Fatalf("RemovePID failed: %v", err)
	}

	// Verify removal
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("expected PID file to be removed, but stat returned: %v", err)
	}
}

func TestIsProcessAlive(t *testing.T) {
	// Current process must be alive
	currentPID := os.Getpid()
	if !IsProcessAlive(currentPID) {
		t.Errorf("expected current process PID %d to be alive", currentPID)
	}

	// PID <= 0 must be dead
	if IsProcessAlive(0) {
		t.Errorf("expected PID 0 to be not alive")
	}
	if IsProcessAlive(-1) {
		t.Errorf("expected PID -1 to be not alive")
	}

	// A non-existent high PID should not be alive
	if IsProcessAlive(99999999) {
		t.Errorf("expected high PID 99999999 to not be alive")
	}
}

func TestRunDaemonLoop_Cancellation(t *testing.T) {
	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		resp := EdgeVersionResponse{
			Version:    "v0.1.9",
			Highlights: []string{"H1", "H2"},
			Status:     "operational",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "edge-version.json")

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := RunDaemonLoop(ctx, server.URL, stateFile, 50*time.Millisecond)
	if err == nil {
		t.Errorf("expected context cancellation error, got nil")
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("expected DeadlineExceeded or Canceled, got: %v", err)
	}

	// State file should have been written
	state, err := ReadState(stateFile)
	if err != nil {
		t.Fatalf("failed to read state after loop: %v", err)
	}
	if state.LatestVersion != "v0.1.9" {
		t.Errorf("state.LatestVersion = %q, want %q", state.LatestVersion, "v0.1.9")
	}
	if state.ServerStatus != "ok" {
		t.Errorf("state.ServerStatus = %q, want %q", state.ServerStatus, "ok")
	}
	if pollCount == 0 {
		t.Errorf("expected server to be polled at least once, got %d", pollCount)
	}
}

func TestGetDaemonStatus(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

	// Empty state
	status, err := GetDaemonStatus()
	if err != nil {
		t.Fatalf("GetDaemonStatus failed: %v", err)
	}
	if status.Active {
		t.Errorf("expected daemon to be inactive initially, got active")
	}
	if status.Mode != "inactive" {
		t.Errorf("expected mode inactive, got %s", status.Mode)
	}

	// Write mock PID file pointing to current process
	pidFile := filepath.Join(tempDir, ".manova", "worker.pid")
	_ = WritePID(pidFile, os.Getpid())

	// Write mock state file
	stateFile := filepath.Join(tempDir, ".manova", "edge-version.json")
	_ = WriteStateAtomic(stateFile, &EdgeVersionState{
		LatestVersion: "v0.2.0",
		ServerStatus:  "ok",
		LastCheckedAt: time.Now().UTC(),
	})

	status, err = GetDaemonStatus()
	if err != nil {
		t.Fatalf("GetDaemonStatus with PID failed: %v", err)
	}
	if !status.Active {
		t.Errorf("expected daemon to be active with alive PID")
	}
	if status.Mode != "detached" {
		t.Errorf("expected mode detached, got %s", status.Mode)
	}
	if status.PID != os.Getpid() {
		t.Errorf("expected status PID %d, got %d", os.Getpid(), status.PID)
	}
	if status.LatestVersion != "v0.2.0" {
		t.Errorf("expected latest version v0.2.0, got %s", status.LatestVersion)
	}
	if status.ServerStatus != "ok" {
		t.Errorf("expected server status ok, got %s", status.ServerStatus)
	}
}
