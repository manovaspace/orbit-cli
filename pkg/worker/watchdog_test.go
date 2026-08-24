package worker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckWatchdog_MissingFile(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentPath := filepath.Join(tempDir, "missing-edge-version.json")

	needsHealing, state, err := CheckWatchdog(nonExistentPath, 5*time.Minute)
	if err != nil {
		t.Errorf("expected err == nil, got %v", err)
	}
	if !needsHealing {
		t.Errorf("expected needsHealing == true for missing file")
	}
	if state != nil {
		t.Errorf("expected state == nil for missing file, got %+v", state)
	}
}

func TestCheckWatchdog_CorruptFile(t *testing.T) {
	tempDir := t.TempDir()
	corruptPath := filepath.Join(tempDir, "corrupt-edge-version.json")
	if err := os.WriteFile(corruptPath, []byte("invalid-json-content"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	needsHealing, state, err := CheckWatchdog(corruptPath, 5*time.Minute)
	if err != nil {
		t.Errorf("expected err == nil, got %v", err)
	}
	if !needsHealing {
		t.Errorf("expected needsHealing == true for corrupt file")
	}
	if state != nil {
		t.Errorf("expected state == nil for corrupt file, got %+v", state)
	}
}

func TestCheckWatchdog_FreshFile(t *testing.T) {
	tempDir := t.TempDir()
	freshPath := filepath.Join(tempDir, "fresh-edge-version.json")

	originalState := &EdgeVersionState{
		LatestVersion: "v0.2.0",
		ServerStatus:  "ok",
		LastCheckedAt: time.Now().UTC().Add(-2 * time.Minute), // 2 min ago (< 5 min default)
		Highlights:    []string{"Feature A", "Feature B"},
	}

	if err := WriteStateAtomic(freshPath, originalState); err != nil {
		t.Fatalf("WriteStateAtomic failed: %v", err)
	}

	needsHealing, state, err := CheckWatchdog(freshPath, 5*time.Minute)
	if err != nil {
		t.Errorf("expected err == nil, got %v", err)
	}
	if needsHealing {
		t.Errorf("expected needsHealing == false for fresh file (checked 2m ago)")
	}
	if state == nil {
		t.Fatalf("expected non-nil state")
	}
	if state.LatestVersion != "v0.2.0" {
		t.Errorf("expected LatestVersion 'v0.2.0', got %q", state.LatestVersion)
	}
	if len(state.Highlights) != 2 {
		t.Errorf("expected 2 highlights, got %d", len(state.Highlights))
	}
}

func TestCheckWatchdog_StaleFile(t *testing.T) {
	tempDir := t.TempDir()
	stalePath := filepath.Join(tempDir, "stale-edge-version.json")

	originalState := &EdgeVersionState{
		LatestVersion: "v0.2.0",
		ServerStatus:  "ok",
		LastCheckedAt: time.Now().UTC().Add(-7 * time.Minute), // 7 min ago (> 5 min default)
		Highlights:    []string{"Feature A"},
	}

	if err := WriteStateAtomic(stalePath, originalState); err != nil {
		t.Fatalf("WriteStateAtomic failed: %v", err)
	}

	needsHealing, state, err := CheckWatchdog(stalePath, 5*time.Minute)
	if err != nil {
		t.Errorf("expected err == nil, got %v", err)
	}
	if !needsHealing {
		t.Errorf("expected needsHealing == true for stale file (checked 7m ago)")
	}
	if state == nil {
		t.Fatalf("expected non-nil state to be returned for stale file")
	}
	if state.LatestVersion != "v0.2.0" {
		t.Errorf("expected LatestVersion 'v0.2.0', got %q", state.LatestVersion)
	}
}

func TestCheckWatchdog_ZeroTimestamp(t *testing.T) {
	tempDir := t.TempDir()
	zeroPath := filepath.Join(tempDir, "zero-edge-version.json")

	originalState := &EdgeVersionState{
		LatestVersion: "v0.2.0",
		ServerStatus:  "ok",
		LastCheckedAt: time.Time{}, // Zero timestamp
	}

	if err := WriteStateAtomic(zeroPath, originalState); err != nil {
		t.Fatalf("WriteStateAtomic failed: %v", err)
	}

	needsHealing, state, err := CheckWatchdog(zeroPath, 5*time.Minute)
	if err != nil {
		t.Errorf("expected err == nil, got %v", err)
	}
	if !needsHealing {
		t.Errorf("expected needsHealing == true for zero timestamp")
	}
	if state == nil {
		t.Fatalf("expected non-nil state")
	}
}

func TestCheckWatchdog_CustomThreshold(t *testing.T) {
	tempDir := t.TempDir()
	customPath := filepath.Join(tempDir, "custom-edge-version.json")

	originalState := &EdgeVersionState{
		LatestVersion: "v0.2.0",
		ServerStatus:  "ok",
		LastCheckedAt: time.Now().UTC().Add(-20 * time.Second),
	}

	if err := WriteStateAtomic(customPath, originalState); err != nil {
		t.Fatalf("WriteStateAtomic failed: %v", err)
	}

	// With 30s threshold -> should not need healing (20s < 30s)
	needsHealing, _, err := CheckWatchdog(customPath, 30*time.Second)
	if err != nil || needsHealing {
		t.Errorf("expected needsHealing == false with 30s threshold, got %v (err: %v)", needsHealing, err)
	}

	// With 10s threshold -> should need healing (20s > 10s)
	needsHealing, _, err = CheckWatchdog(customPath, 10*time.Second)
	if err != nil || !needsHealing {
		t.Errorf("expected needsHealing == true with 10s threshold, got %v (err: %v)", needsHealing, err)
	}
}

func TestHealWorker_Sync(t *testing.T) {
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EdgeVersionResponse{
			Version:    "v0.2.0",
			Highlights: []string{"Item 1", "Item 2"},
			Status:     "operational",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	statePath := filepath.Join(tempDir, "edge-version.json")

	err := HealWorker("", server.URL, statePath)
	if err != nil {
		t.Errorf("HealWorker returned error: %v", err)
	}

	st, err := ReadState(statePath)
	if err != nil {
		t.Fatalf("ReadState failed after HealWorker: %v", err)
	}
	if st.LatestVersion != "v0.2.0" {
		t.Errorf("expected LatestVersion 'v0.2.0', got %q", st.LatestVersion)
	}
	if st.ServerStatus != "ok" {
		t.Errorf("expected ServerStatus 'ok', got %q", st.ServerStatus)
	}
}

func TestHealWorkerBackground_NonBlocking(t *testing.T) {
	t.Setenv("MANOVA_FORCE_DETACHED", "1")
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	start := time.Now()
	HealWorkerBackground("")
	elapsed := time.Since(start)

	// HealWorkerBackground must return immediately in < 50ms without blocking
	if elapsed > 100*time.Millisecond {
		t.Errorf("HealWorkerBackground took too long (%v), expected non-blocking call", elapsed)
	}
}
