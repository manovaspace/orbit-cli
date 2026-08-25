package notifier

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FetchFeed fetches and parses the /api/feed JSON from endpoint.
func FetchFeed(endpoint string) (*FeedResponse, error) {
	if endpoint == "" {
		endpoint = DefaultFeedURL
	}
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("feed request build failed: %w", err)
	}
	req.Header.Set("User-Agent", "manova-notifier/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed server returned %d", resp.StatusCode)
	}

	var feed FeedResponse
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("feed decode failed: %w", err)
	}
	return &feed, nil
}

// expandPath expands leading ~ to the user home directory.
func expandPath(path string) string {
	if path == "" {
		path = DefaultFeedFile
	}
	if len(path) >= 2 && path[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ReadFeedState reads the persisted FeedState from statePath.
func ReadFeedState(statePath string) (*FeedState, error) {
	data, err := os.ReadFile(expandPath(statePath))
	if err != nil {
		return nil, err
	}
	var s FeedState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("feed state parse failed: %w", err)
	}
	return &s, nil
}

// WriteFeedStateAtomic writes FeedState atomically via temp-file rename.
func WriteFeedStateAtomic(statePath string, state *FeedState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}
	dst := expandPath(statePath)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".feed-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Chmod(0644)
	_ = tmp.Sync()
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

// PollFeed fetches the /api/feed, updates persisted FeedState, and returns it.
// On failure it preserves the last known state and marks server as "down".
func PollFeed(endpoint, statePath string) (*FeedState, error) {
	existing, _ := ReadFeedState(statePath)
	if existing == nil {
		existing = &FeedState{LatestVersion: "dev"}
	}

	feed, err := FetchFeed(endpoint)
	if err != nil {
		existing.ServerStatus = "down"
		existing.LastError = err.Error()
		existing.LastCheckedAt = time.Now().UTC()
		_ = WriteFeedStateAtomic(statePath, existing)
		return existing, err
	}

	existing.LatestVersion = feed.Version
	existing.Messages = feed.Messages
	existing.ServerStatus = "ok"
	existing.LastError = ""
	existing.LastCheckedAt = time.Now().UTC()

	if err := WriteFeedStateAtomic(statePath, existing); err != nil {
		return existing, fmt.Errorf("failed to persist feed state: %w", err)
	}
	return existing, nil
}
