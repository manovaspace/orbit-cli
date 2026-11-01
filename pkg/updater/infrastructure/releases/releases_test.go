package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

func TestCheckUpdateMock(t *testing.T) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	mockRelease := apiRelease{
		ID:          101,
		TagName:     "v1.5.0",
		Name:        "Release 1.5.0",
		Body:        "## Changelog\n- Added multi-repo sync\n- Added doctor diagnostics",
		HTMLURL:     "https://github.com/manovaspace/orbit-cli/releases/tag/v1.5.0",
		CreatedAt:   time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		PublishedAt: time.Date(2026, 8, 20, 10, 30, 0, 0, time.UTC),
		Assets: []apiAsset{
			{
				ID:                 1,
				Name:               fmt.Sprintf("manova_1.5.0_%s_%s.tar.gz", goos, goarch),
				BrowserDownloadURL: fmt.Sprintf("https://example.com/attachments/manova_1.5.0_%s_%s.tar.gz", goos, goarch),
			},
			{
				ID:                 2,
				Name:               "manova_1.5.0_windows_amd64.zip",
				BrowserDownloadURL: "https://example.com/attachments/manova_1.5.0_windows_amd64.zip",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockRelease)
	}))
	defer server.Close()

	res, err := New().Check(context.Background(), "v1.4.0", server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.HasUpdate {
		t.Errorf("expected HasUpdate = true, got false")
	}
	if res.LatestVersion != "1.5.0" {
		t.Errorf("expected LatestVersion = '1.5.0', got %q", res.LatestVersion)
	}
	if res.CurrentVersion != "v1.4.0" {
		t.Errorf("expected CurrentVersion = 'v1.4.0', got %q", res.CurrentVersion)
	}
	if res.Release == nil {
		t.Fatal("expected non-nil Release")
	}
	expectedAsset := fmt.Sprintf("https://example.com/attachments/manova_1.5.0_%s_%s.tar.gz", goos, goarch)
	if res.Release.AssetURL != expectedAsset {
		t.Errorf("expected AssetURL = %q, got %q", expectedAsset, res.Release.AssetURL)
	}

	resNewer, err := New().Check(context.Background(), "v1.6.0", server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resNewer.HasUpdate {
		t.Errorf("expected HasUpdate = false for current v1.6.0 vs latest v1.5.0")
	}
}

func TestCheckUpdateErrorCases(t *testing.T) {
	s404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	defer s404.Close()
	if _, err := New().Check(context.Background(), "v1.0.0", s404.URL, "manova/orbit-cli"); err == nil {
		t.Error("expected error on 404, got nil")
	}

	sBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json output"))
	}))
	defer sBadJSON.Close()
	if _, err := New().Check(context.Background(), "v1.0.0", sBadJSON.URL, "manova/orbit-cli"); err == nil {
		t.Error("expected error on invalid JSON, got nil")
	}

	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedServer.URL
	closedServer.Close()
	if _, err := New().Check(context.Background(), "v1.0.0", closedURL, "manova/orbit-cli"); err == nil {
		t.Error("expected error on unreachable server, got nil")
	}
}

func TestCheckUpdateCached(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "sub", "update-check.json")

	var serverHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiRelease{
			TagName:     "v1.8.0",
			Body:        "Cached test release",
			PublishedAt: time.Now(),
		})
	}))
	defer server.Close()

	checker := New()
	res1, err := checker.CheckCached(context.Background(), "v1.0.0", cachePath, 24*time.Hour, server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	if serverHits != 1 {
		t.Fatalf("expected 1 server hit, got %d", serverHits)
	}
	if !res1.HasUpdate || res1.LatestVersion != "1.8.0" {
		t.Errorf("expected update to 1.8.0, got %+v", res1)
	}
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatalf("expected cache file at %s, but does not exist", cachePath)
	}

	res2, err := checker.CheckCached(context.Background(), "v1.0.0", cachePath, 24*time.Hour, server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if serverHits != 1 {
		t.Errorf("expected serverHits to remain 1 on cache hit, got %d", serverHits)
	}
	if !res2.HasUpdate || res2.LatestVersion != "1.8.0" {
		t.Errorf("expected cached update to 1.8.0, got %+v", res2)
	}

	res3, err := checker.CheckCached(context.Background(), "v1.8.0", cachePath, 24*time.Hour, server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error on third call: %v", err)
	}
	if serverHits != 1 {
		t.Errorf("expected serverHits to remain 1, got %d", serverHits)
	}
	if res3.HasUpdate {
		t.Errorf("expected HasUpdate = false when currentVersion matches latest cached version")
	}

	time.Sleep(5 * time.Millisecond)
	res4, err := checker.CheckCached(context.Background(), "v1.0.0", cachePath, 1*time.Millisecond, server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error on expired call: %v", err)
	}
	if serverHits != 2 {
		t.Errorf("expected serverHits to be 2 after cache expiration, got %d", serverHits)
	}
	if !res4.HasUpdate {
		t.Errorf("expected HasUpdate = true on refreshed cache")
	}
}

func TestExpandCachePath(t *testing.T) {
	def := ExpandCachePath("")
	if !strings.HasSuffix(def, filepath.Join(".orbit", "update-check.json")) {
		t.Errorf("expected default cache path to end with .orbit/update-check.json, got %q", def)
	}

	abs := "/tmp/test/cache.json"
	if ExpandCachePath(abs) != abs {
		t.Errorf("expected absolute path to remain unchanged, got %q", ExpandCachePath(abs))
	}

	tilde := "~/custom/cache.json"
	expanded := ExpandCachePath(tilde)
	if strings.HasPrefix(expanded, "~") {
		t.Errorf("expected tilde to be expanded, got %q", expanded)
	}
}

// mockSource implements selfupdate.Source for testing SelfUpdate logic.
type mockSource struct {
	releases []selfupdate.SourceRelease
	err      error
}

func (m *mockSource) ListReleases(_ context.Context, _ selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.releases, nil
}

func (m *mockSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, _ int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("dummy binary content")), nil
}

func TestSelfUpdateWithMockSource(t *testing.T) {
	checker := &Checker{Source: &mockSource{releases: []selfupdate.SourceRelease{}}}
	if err := checker.SelfUpdate(context.Background(), "v1.0.0", "manova/orbit-cli"); err != nil {
		t.Errorf("expected no error when no newer releases exist, got: %v", err)
	}

	errorChecker := &Checker{Source: &mockSource{err: fmt.Errorf("mock network failure")}}
	if err := errorChecker.SelfUpdate(context.Background(), "v1.0.0", "manova/orbit-cli"); err == nil {
		t.Error("expected error from failing source, got nil")
	}
}
