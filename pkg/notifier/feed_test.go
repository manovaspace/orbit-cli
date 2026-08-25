package notifier_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/notifier"
)

func TestFetchFeed_Success(t *testing.T) {
	expires := time.Now().Add(24 * time.Hour)
	feed := notifier.FeedResponse{
		Version: "v0.2.4",
		Status:  "operational",
		Messages: []notifier.Message{
			{ID: "release-v0.2.4", Type: "release", Priority: "high", Title: "v0.2.4"},
			{ID: "tip-1", Type: "tip", Priority: "low", Title: "a tip", ExpiresAt: &expires},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(feed)
	}))
	defer srv.Close()

	result, err := notifier.FetchFeed(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Version != "v0.2.4" {
		t.Errorf("expected version v0.2.4, got %s", result.Version)
	}
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result.Messages))
	}
}

func TestFetchFeed_NetworkError(t *testing.T) {
	_, err := notifier.FetchFeed("http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
}

func TestPollFeed_WritesStateFile(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "feed.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feed := notifier.FeedResponse{Version: "v0.2.4", Status: "operational"}
		json.NewEncoder(w).Encode(feed)
	}))
	defer srv.Close()

	state, err := notifier.PollFeed(srv.URL, stateFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.LatestVersion != "v0.2.4" {
		t.Errorf("expected v0.2.4, got %s", state.LatestVersion)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Errorf("state file not written: %v", err)
	}
}

func TestPollFeed_PreservesVersionOnServerDown(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "feed.json")

	seed := notifier.FeedState{LatestVersion: "v0.2.3", ServerStatus: "ok"}
	data, _ := json.MarshalIndent(seed, "", "  ")
	os.WriteFile(stateFile, data, 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	state, _ := notifier.PollFeed(srv.URL, stateFile)
	if state.LatestVersion != "v0.2.3" {
		t.Errorf("expected preserved v0.2.3, got %s", state.LatestVersion)
	}
	if state.ServerStatus != "down" {
		t.Errorf("expected status down, got %s", state.ServerStatus)
	}
}
