package worker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExpandPath expands leading ~ to user's home directory and applies DefaultStateFile if empty.
func ExpandPath(path string) string {
	if path == "" {
		path = DefaultStateFile
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ReadState reads and deserializes the EdgeVersionState from statePath.
func ReadState(statePath string) (*EdgeVersionState, error) {
	expanded := ExpandPath(statePath)
	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, err
	}

	var state EdgeVersionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// WriteStateAtomic writes state to a temporary file in the same directory as statePath
// and performs an atomic rename to guarantee consistency and 0644 file permissions.
func WriteStateAtomic(statePath string, state *EdgeVersionState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	expanded := ExpandPath(statePath)
	dir := filepath.Dir(expanded)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	data = append(data, '\n')

	tmpFile, err := os.CreateTemp(dir, ".edge-version-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp state: %w", err)
	}

	if err := tmpFile.Chmod(0644); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to chmod temp state: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp state: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp state: %w", err)
	}

	if err := os.Rename(tmpPath, expanded); err != nil {
		return fmt.Errorf("failed to rename temp state to %q: %w", expanded, err)
	}

	cleanup = false
	return nil
}

// PollOnce performs a single version check against endpoint with a 5-second timeout.
// It updates statePath atomically. On network or server failures, it preserves
// the last known valid version, marks server_status as "down", and logs the error.
func PollOnce(endpoint, statePath string) (*EdgeVersionState, error) {
	return PollOnceWithTimeout(endpoint, statePath, 5*time.Second)
}

// PollOnceWithTimeout performs a single version check against endpoint with the specified timeout.
func PollOnceWithTimeout(endpoint, statePath string, timeout time.Duration) (*EdgeVersionState, error) {
	if endpoint == "" {
		endpoint = DefaultEdgeURL
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	expandedPath := ExpandPath(statePath)

	state, _ := ReadState(expandedPath)
	if state == nil {
		state = &EdgeVersionState{
			LatestVersion: "dev",
		}
	}
	if state.LatestVersion == "" {
		state.LatestVersion = "dev"
	}

	fail := func(err error) (*EdgeVersionState, error) {
		state.ServerStatus = "down"
		state.LastError = err.Error()
		state.LastCheckedAt = time.Now().UTC()
		if state.LatestVersion == "" {
			state.LatestVersion = "dev"
		}
		_ = WriteStateAtomic(expandedPath, state)
		return state, err
	}

	client := &http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fail(fmt.Errorf("failed to create request: %w", err))
	}
	req.Header.Set("User-Agent", "manova-worker/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fail(fmt.Errorf("failed to reach edge server: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("edge server returned status %d: %s", resp.StatusCode, resp.Status))
	}

	var edgeResp EdgeVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&edgeResp); err != nil {
		return fail(fmt.Errorf("failed to decode edge version response: %w", err))
	}

	version := strings.TrimSpace(edgeResp.Version)
	if version == "" {
		version = strings.TrimSpace(edgeResp.TagName)
	}
	if version != "" {
		state.LatestVersion = version
	}

	state.Highlights = edgeResp.Highlights
	state.ServerStatus = "ok"
	state.LastError = ""
	state.LastCheckedAt = time.Now().UTC()

	if err := WriteStateAtomic(expandedPath, state); err != nil {
		return state, fmt.Errorf("failed to persist state: %w", err)
	}

	return state, nil
}
