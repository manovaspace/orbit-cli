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

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home dir: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty path defaults to DefaultStateFile",
			input:    "",
			expected: filepath.Join(home, ".manova", "edge-version.json"),
		},
		{
			name:     "tilde expansion",
			input:    "~/.manova/custom-version.json",
			expected: filepath.Join(home, ".manova", "custom-version.json"),
		},
		{
			name:     "tilde only",
			input:    "~",
			expected: home,
		},
		{
			name:     "absolute path",
			input:    "/tmp/test-state.json",
			expected: "/tmp/test-state.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.input)
			if got != tt.expected {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestWriteStateAtomic_AndRead(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "subdir", "edge-version.json")

	now := time.Now().UTC().Truncate(time.Second)
	state := &EdgeVersionState{
		LatestVersion: "v0.1.9",
		LastCheckedAt: now,
		ServerStatus:  "ok",
		LastError:     "",
		WorkerStatus:  "running",
		WorkerPID:     12345,
		WorkerMode:    "detached",
		Highlights: []string{
			"Feature 1",
			"Feature 2",
		},
	}

	// Test writing
	err := WriteStateAtomic(stateFile, state)
	if err != nil {
		t.Fatalf("WriteStateAtomic failed: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(stateFile)
	if err != nil {
		t.Fatalf("os.Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Errorf("expected permissions 0644, got %o", perm)
	}

	// Test reading back
	readBack, err := ReadState(stateFile)
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}

	if readBack.LatestVersion != state.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", readBack.LatestVersion, state.LatestVersion)
	}
	if readBack.ServerStatus != state.ServerStatus {
		t.Errorf("ServerStatus = %q, want %q", readBack.ServerStatus, state.ServerStatus)
	}
	if readBack.LastError != state.LastError {
		t.Errorf("LastError = %q, want %q", readBack.LastError, state.LastError)
	}
	if readBack.WorkerStatus != state.WorkerStatus {
		t.Errorf("WorkerStatus = %q, want %q", readBack.WorkerStatus, state.WorkerStatus)
	}
	if readBack.WorkerPID != state.WorkerPID {
		t.Errorf("WorkerPID = %d, want %d", readBack.WorkerPID, state.WorkerPID)
	}
	if readBack.WorkerMode != state.WorkerMode {
		t.Errorf("WorkerMode = %q, want %q", readBack.WorkerMode, state.WorkerMode)
	}
	if len(readBack.Highlights) != len(state.Highlights) {
		t.Errorf("Highlights len = %d, want %d", len(readBack.Highlights), len(state.Highlights))
	} else {
		for i, h := range readBack.Highlights {
			if h != state.Highlights[i] {
				t.Errorf("Highlights[%d] = %q, want %q", i, h, state.Highlights[i])
			}
		}
	}
}

func TestWriteStateAtomic_NilState(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "edge-version.json")

	if err := WriteStateAtomic(stateFile, nil); err == nil {
		t.Errorf("expected error when state is nil, got nil")
	}
}

func TestReadState_NonExistentAndCorrupt(t *testing.T) {
	tempDir := t.TempDir()
	nonExistent := filepath.Join(tempDir, "does-not-exist.json")

	if _, err := ReadState(nonExistent); err == nil {
		t.Errorf("expected error for non-existent file, got nil")
	}

	corruptFile := filepath.Join(tempDir, "corrupt.json")
	if err := os.WriteFile(corruptFile, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	if _, err := ReadState(corruptFile); err == nil {
		t.Errorf("expected parse error for corrupt file, got nil")
	}
}

func TestPollOnce_Success(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "edge-version.json")

	mockResp := EdgeVersionResponse{
		Version:     "v0.1.9",
		TagName:     "v0.1.9",
		PublishedAt: time.Now().UTC(),
		Highlights: []string{
			"Auto-configure shell autocompletion for bash and zsh",
			"Link tab-completion to optional 'm' shortcut alias",
		},
		DownloadURLTemplate: "https://github.com/manovaspace/orbit-cli/releases/download/{tag}/manova-{os}-{arch}",
		Status:              "operational",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(mockResp)
	}))
	defer server.Close()

	state, err := PollOnce(server.URL, stateFile)
	if err != nil {
		t.Fatalf("PollOnce failed: %v", err)
	}

	if state.LatestVersion != "v0.1.9" {
		t.Errorf("LatestVersion = %q, want %q", state.LatestVersion, "v0.1.9")
	}
	if state.ServerStatus != "ok" {
		t.Errorf("ServerStatus = %q, want %q", state.ServerStatus, "ok")
	}
	if state.LastError != "" {
		t.Errorf("LastError = %q, want empty string", state.LastError)
	}
	if state.LastCheckedAt.IsZero() {
		t.Errorf("LastCheckedAt should not be zero")
	}
	if len(state.Highlights) != 2 {
		t.Errorf("Highlights length = %d, want 2", len(state.Highlights))
	}

	// Verify state file persisted to disk
	persisted, err := ReadState(stateFile)
	if err != nil {
		t.Fatalf("failed to read persisted state file: %v", err)
	}
	if persisted.LatestVersion != "v0.1.9" {
		t.Errorf("Persisted LatestVersion = %q, want %q", persisted.LatestVersion, "v0.1.9")
	}
	if persisted.ServerStatus != "ok" {
		t.Errorf("Persisted ServerStatus = %q, want %q", persisted.ServerStatus, "ok")
	}
}

func TestPollOnce_ServerDownPreservesVersion(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "edge-version.json")

	// Seed existing state with v0.1.9
	initialState := &EdgeVersionState{
		LatestVersion: "v0.1.9",
		LastCheckedAt: time.Now().UTC().Add(-10 * time.Minute),
		ServerStatus:  "ok",
		LastError:     "",
		WorkerStatus:  "running",
		Highlights: []string{
			"Initial feature",
		},
	}
	if err := WriteStateAtomic(stateFile, initialState); err != nil {
		t.Fatalf("failed to seed initial state: %v", err)
	}

	// Create mock server that returns 500 Internal Server Error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("500 Internal Server Error"))
	}))
	defer server.Close()

	state, err := PollOnce(server.URL, stateFile)
	if err == nil {
		t.Fatalf("expected error from PollOnce when server returns 500, got nil")
	}

	// Verify in-memory returned state
	if state == nil {
		t.Fatalf("expected non-nil state returned even on error")
	}
	if state.LatestVersion != "v0.1.9" {
		t.Errorf("LatestVersion = %q, want preserved %q", state.LatestVersion, "v0.1.9")
	}
	if state.ServerStatus != "down" {
		t.Errorf("ServerStatus = %q, want %q", state.ServerStatus, "down")
	}
	if state.LastError == "" {
		t.Errorf("LastError should not be empty")
	}
	if len(state.Highlights) != 1 || state.Highlights[0] != "Initial feature" {
		t.Errorf("Highlights = %v, want preserved %v", state.Highlights, initialState.Highlights)
	}

	// Verify persisted state on disk
	persisted, err := ReadState(stateFile)
	if err != nil {
		t.Fatalf("failed to read persisted state: %v", err)
	}
	if persisted.LatestVersion != "v0.1.9" {
		t.Errorf("Persisted LatestVersion = %q, want %q", persisted.LatestVersion, "v0.1.9")
	}
	if persisted.ServerStatus != "down" {
		t.Errorf("Persisted ServerStatus = %q, want %q", persisted.ServerStatus, "down")
	}
	if persisted.LastError == "" {
		t.Errorf("Persisted LastError should not be empty")
	}
}

func TestPollOnce_InvalidJSONResponse(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "edge-version.json")

	// Create mock server that returns 200 OK but broken JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	state, err := PollOnce(server.URL, stateFile)
	if err == nil {
		t.Fatalf("expected error from PollOnce when JSON is invalid, got nil")
	}

	if state.ServerStatus != "down" {
		t.Errorf("ServerStatus = %q, want %q", state.ServerStatus, "down")
	}
	if state.LastError == "" {
		t.Errorf("LastError should be populated")
	}
}

func TestPollOnce_NetworkFailureNoInitialState(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "edge-version.json")

	// Target an endpoint that immediately fails (closed port)
	state, err := PollOnce("http://127.0.0.1:59999/version", stateFile)
	if err == nil {
		t.Fatalf("expected error for closed port, got nil")
	}

	if state == nil {
		t.Fatalf("expected non-nil state on error")
	}
	if state.LatestVersion != "dev" {
		t.Errorf("LatestVersion = %q, want default %q", state.LatestVersion, "dev")
	}
	if state.ServerStatus != "down" {
		t.Errorf("ServerStatus = %q, want %q", state.ServerStatus, "down")
	}
	if state.LastError == "" {
		t.Errorf("LastError should record failure")
	}

	// Verify file was written even on cold-start failure
	persisted, err := ReadState(stateFile)
	if err != nil {
		t.Fatalf("expected persisted state file, got error: %v", err)
	}
	if persisted.ServerStatus != "down" {
		t.Errorf("Persisted ServerStatus = %q, want %q", persisted.ServerStatus, "down")
	}
}
