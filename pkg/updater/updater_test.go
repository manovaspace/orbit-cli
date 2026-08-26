package updater

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

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		target   string
		expected bool
	}{
		{
			name:     "minor version upgrade",
			current:  "v1.0.0",
			target:   "v1.1.0",
			expected: true,
		},
		{
			name:     "patch version downgrade",
			current:  "v1.2.0",
			target:   "v1.1.5",
			expected: false,
		},
		{
			name:     "dev build against release",
			current:  "dev",
			target:   "v1.0.0",
			expected: true,
		},
		{
			name:     "dev build against dev build",
			current:  "dev",
			target:   "dev",
			expected: false,
		},
		{
			name:     "release against dev build",
			current:  "v1.0.0",
			target:   "dev",
			expected: false,
		},
		{
			name:     "identical versions with v prefix",
			current:  "v1.0.0",
			target:   "v1.0.0",
			expected: false,
		},
		{
			name:     "identical versions without v prefix",
			current:  "1.0.0",
			target:   "1.0.0",
			expected: false,
		},
		{
			name:     "mixed v prefix comparison",
			current:  "1.0.0",
			target:   "v1.0.1",
			expected: true,
		},
		{
			name:     "major version upgrade",
			current:  "v1.9.9",
			target:   "v2.0.0",
			expected: true,
		},
		{
			name:     "prerelease upgrade to stable",
			current:  "v1.0.0-rc.1",
			target:   "v1.0.0",
			expected: true,
		},
		{
			name:     "stable to older prerelease",
			current:  "v1.0.0",
			target:   "v1.0.0-rc.1",
			expected: false,
		},
		{
			name:     "empty current version against valid release",
			current:  "",
			target:   "v1.0.0",
			expected: true,
		},
		{
			name:     "empty target version",
			current:  "v1.0.0",
			target:   "",
			expected: false,
		},
		{
			name:     "invalid semver strings",
			current:  "invalid-cur",
			target:   "invalid-tgt",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := IsNewerVersion(tt.current, tt.target)
			if actual != tt.expected {
				t.Errorf("IsNewerVersion(%q, %q) = %v; want %v", tt.current, tt.target, actual, tt.expected)
			}
		})
	}
}

func TestResolveAPIEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		apiURL   string
		repoSlug string
		expected string
	}{
		{
			name:     "empty defaults to GitHub",
			apiURL:   "",
			repoSlug: "",
			expected: "https://api.github.com/repos/manovaspace/orbit-cli/releases/latest",
		},
		{
			name:     "custom Forgejo with trailing slash",
			apiURL:   "https://git.example.com/",
			repoSlug: "org/repo",
			expected: "https://git.example.com/api/v1/repos/org/repo/releases/latest",
		},
		{
			name:     "custom Forgejo already having /api/v1",
			apiURL:   "https://git.example.com/api/v1",
			repoSlug: "org/repo",
			expected: "https://git.example.com/api/v1/repos/org/repo/releases/latest",
		},
		{
			name:     "standard github.com",
			apiURL:   "https://github.com",
			repoSlug: "manovaspace/orbit-cli",
			expected: "https://api.github.com/repos/manovaspace/orbit-cli/releases/latest",
		},
		{
			name:     "api.github.com",
			apiURL:   "https://api.github.com",
			repoSlug: "manovaspace/orbit-cli",
			expected: "https://api.github.com/repos/manovaspace/orbit-cli/releases/latest",
		},
		{
			name:     "exact releases latest URL",
			apiURL:   "http://localhost:8080/custom/releases/latest",
			repoSlug: "ignored/repo",
			expected: "http://localhost:8080/custom/releases/latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := ResolveAPIEndpoint(tt.apiURL, tt.repoSlug)
			if actual != tt.expected {
				t.Errorf("ResolveAPIEndpoint(%q, %q) = %q; want %q", tt.apiURL, tt.repoSlug, actual, tt.expected)
			}
		})
	}
}

func TestCheckUpdate_ForgejoMock(t *testing.T) {
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
				BrowserDownloadURL: fmt.Sprintf("https://git.dev.manova.space/attachments/manova_1.5.0_%s_%s.tar.gz", goos, goarch),
			},
			{
				ID:                 2,
				Name:               "manova_1.5.0_windows_amd64.zip",
				BrowserDownloadURL: "https://git.dev.manova.space/attachments/manova_1.5.0_windows_amd64.zip",
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

	// Test case 1: Current version is older
	res, err := CheckUpdate("v1.4.0", server.URL, "manova/orbit-cli")
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
	if res.Release.TagName != "v1.5.0" {
		t.Errorf("expected TagName = 'v1.5.0', got %q", res.Release.TagName)
	}
	expectedAsset := fmt.Sprintf("https://git.dev.manova.space/attachments/manova_1.5.0_%s_%s.tar.gz", goos, goarch)
	if res.Release.AssetURL != expectedAsset {
		t.Errorf("expected AssetURL = %q, got %q", expectedAsset, res.Release.AssetURL)
	}
	if !strings.Contains(res.Release.ReleaseNotes, "doctor diagnostics") {
		t.Errorf("expected ReleaseNotes to contain 'doctor diagnostics', got %q", res.Release.ReleaseNotes)
	}

	// Test case 2: Current version is already newer
	resNewer, err := CheckUpdate("v1.6.0", server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resNewer.HasUpdate {
		t.Errorf("expected HasUpdate = false for current v1.6.0 vs latest v1.5.0, got true")
	}
}

func TestCheckUpdate_GitHubMock(t *testing.T) {
	mockRelease := apiRelease{
		ID:          202,
		TagName:     "v2.1.0",
		Name:        "v2.1.0",
		Body:        "GitHub release notes",
		HTMLURL:     "https://github.com/manova/orbit-cli/releases/tag/v2.1.0",
		PublishedAt: time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
		Assets: []apiAsset{
			{
				ID:                 10,
				Name:               "manova_universal.tar.gz",
				BrowserDownloadURL: "https://github.com/manova/orbit-cli/releases/download/v2.1.0/manova_universal.tar.gz",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockRelease)
	}))
	defer server.Close()

	res, err := CheckUpdate("v2.0.0", server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.HasUpdate {
		t.Errorf("expected HasUpdate = true, got false")
	}
	if res.LatestVersion != "2.1.0" {
		t.Errorf("expected LatestVersion = '2.1.0', got %q", res.LatestVersion)
	}
	if res.Release.AssetURL != "https://github.com/manova/orbit-cli/releases/download/v2.1.0/manova_universal.tar.gz" {
		t.Errorf("expected first asset URL fallback, got %q", res.Release.AssetURL)
	}
}

func TestCheckUpdate_ErrorCases(t *testing.T) {
	// 404 Not Found
	s404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	defer s404.Close()

	_, err := CheckUpdate("v1.0.0", s404.URL, "manova/orbit-cli")
	if err == nil {
		t.Error("expected error on 404, got nil")
	}

	// 500 Internal Server Error
	s500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer s500.Close()

	_, err = CheckUpdate("v1.0.0", s500.URL, "manova/orbit-cli")
	if err == nil {
		t.Error("expected error on 500, got nil")
	}

	// Invalid JSON
	sBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json output"))
	}))
	defer sBadJSON.Close()

	_, err = CheckUpdate("v1.0.0", sBadJSON.URL, "manova/orbit-cli")
	if err == nil {
		t.Error("expected error on invalid JSON, got nil")
	}

	// Connection refused / closed server
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedServer.URL
	closedServer.Close()

	_, err = CheckUpdate("v1.0.0", closedURL, "manova/orbit-cli")
	if err == nil {
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

	// 1. First call -> Cache miss, hits server and writes cache
	res1, err := CheckUpdateCached("v1.0.0", cachePath, 24*time.Hour, server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	if serverHits != 1 {
		t.Fatalf("expected 1 server hit, got %d", serverHits)
	}
	if !res1.HasUpdate || res1.LatestVersion != "1.8.0" {
		t.Errorf("expected update to 1.8.0, got %+v", res1)
	}

	// Verify file exists on disk
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatalf("expected cache file at %s, but does not exist", cachePath)
	}

	// 2. Second call -> Cache hit within TTL, should NOT hit server
	res2, err := CheckUpdateCached("v1.0.0", cachePath, 24*time.Hour, server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if serverHits != 1 {
		t.Errorf("expected serverHits to remain 1 on cache hit, got %d", serverHits)
	}
	if !res2.HasUpdate || res2.LatestVersion != "1.8.0" {
		t.Errorf("expected cached update to 1.8.0, got %+v", res2)
	}

	// 3. Third call -> Dynamic currentVersion change (currentVersion = "v1.8.0") with cached result
	res3, err := CheckUpdateCached("v1.8.0", cachePath, 24*time.Hour, server.URL, "manova/orbit-cli")
	if err != nil {
		t.Fatalf("unexpected error on third call: %v", err)
	}
	if serverHits != 1 {
		t.Errorf("expected serverHits to remain 1, got %d", serverHits)
	}
	if res3.HasUpdate {
		t.Errorf("expected HasUpdate = false when currentVersion matches latest cached version")
	}

	// 4. Fourth call -> Expired TTL (TTL = 1 nanosecond with a tiny sleep)
	time.Sleep(5 * time.Millisecond)
	res4, err := CheckUpdateCached("v1.0.0", cachePath, 1*time.Millisecond, server.URL, "manova/orbit-cli")
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
	// Empty path
	def := ExpandCachePath("")
	if !strings.HasSuffix(def, filepath.Join(".orbit", "edge-version.json")) {
		t.Errorf("expected default cache path to end with .orbit/edge-version.json, got %q", def)
	}

	// Absolute path
	abs := "/tmp/test/cache.json"
	if ExpandCachePath(abs) != abs {
		t.Errorf("expected absolute path %q to remain unchanged, got %q", abs, ExpandCachePath(abs))
	}

	// Tilde path
	tilde := "~/custom/cache.json"
	expanded := ExpandCachePath(tilde)
	if strings.HasPrefix(expanded, "~") {
		t.Errorf("expected tilde to be expanded, got %q", expanded)
	}
}

// mockSource implements selfupdate.Source for testing SelfUpdate logic
type mockSource struct {
	releases []selfupdate.SourceRelease
	err      error
}

func (m *mockSource) ListReleases(ctx context.Context, repository selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.releases, nil
}

func (m *mockSource) DownloadReleaseAsset(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("dummy binary content")), nil
}

func TestSelfUpdate_WithMockSource(t *testing.T) {
	// Test with a mock source that returns empty releases (already up to date)
	mock := &mockSource{
		releases: []selfupdate.SourceRelease{},
	}

	err := SelfUpdate("v1.0.0", "manova/orbit-cli", mock)
	if err != nil {
		t.Errorf("expected no error when no newer releases exist, got: %v", err)
	}

	// Test with an erroring mock source
	errorMock := &mockSource{
		err: fmt.Errorf("mock network failure"),
	}
	err = SelfUpdate("v1.0.0", "manova/orbit-cli", errorMock)
	if err == nil {
		t.Error("expected error from failing source, got nil")
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0.1.5", "v0.1.5"},
		{"v0.1.5", "v0.1.5"},
		{"vv0.1.5", "v0.1.5"},
		{"dev", "dev"},
		{"vdev", "dev"},
		{"", "dev"},
	}

	for _, tt := range tests {
		actual := FormatVersion(tt.input)
		if actual != tt.expected {
			t.Errorf("FormatVersion(%q) = %q, want %q", tt.input, actual, tt.expected)
		}
	}
}

func TestTruncateReleaseNotes(t *testing.T) {
	rawNotes := `## What's Changed
* Feature 1: Collision-aware shortcut alias
- Feature 2: Automated on-the-fly dependency self-healing
• Feature 3: Direct public self-update via GitHub Releases
1. Feature 4: Interactive token prompt
2. Feature 5: Built-in uninstall command
* Feature 6: Diagnostic report bundle
* Feature 7: Session resumption
`

	highlights := TruncateReleaseNotes(rawNotes, 5)
	if len(highlights) != 6 { // 5 items + 1 truncated summary
		t.Fatalf("expected 6 lines (5 items + 1 summary), got %d: %v", len(highlights), highlights)
	}

	if highlights[0] != "Feature 1: Collision-aware shortcut alias" {
		t.Errorf("unexpected first item: %s", highlights[0])
	}
	if highlights[4] != "Feature 5: Built-in uninstall command" {
		t.Errorf("unexpected fifth item: %s", highlights[4])
	}
	if highlights[5] != "… (+2 more changes)" {
		t.Errorf("unexpected summary line: %s", highlights[5])
	}

	// Test short notes (< 5 items)
	shortNotes := "* Patch A\n* Patch B"
	shortHighlights := TruncateReleaseNotes(shortNotes, 5)
	if len(shortHighlights) != 2 {
		t.Errorf("expected 2 items, got %d", len(shortHighlights))
	}
}

func TestCleanMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"**Bold Title**", "Bold Title"},
		{"`command --flag`", "command --flag"},
		{"**Title:** with `code` block", "Title: with code block"},
		{"Plain text", "Plain text"},
	}

	for _, tt := range tests {
		actual := CleanMarkdown(tt.input)
		if actual != tt.expected {
			t.Errorf("CleanMarkdown(%q) = %q, want %q", tt.input, actual, tt.expected)
		}
	}
}

func TestFormatTerminalHighlight(t *testing.T) {
	input1 := "**Styled Release Cards:** Each changelog entry renders in a box."
	formatted1 := FormatTerminalHighlight(input1)
	if !strings.Contains(formatted1, "Styled Release Cards:") || !strings.Contains(formatted1, "Each changelog entry renders in a box.") || strings.Contains(formatted1, "**") {
		t.Errorf("unexpected FormatTerminalHighlight output: %q", formatted1)
	}

	input2 := "Auto-Pager: Output is automatically paged through `less -RF`."
	formatted2 := FormatTerminalHighlight(input2)
	if !strings.Contains(formatted2, "Auto-Pager:") || !strings.Contains(formatted2, "less -RF") || strings.Contains(formatted2, "`") {
		t.Errorf("unexpected FormatTerminalHighlight output: %q", formatted2)
	}

	input3 := "… (+2 more changes)"
	formatted3 := FormatTerminalHighlight(input3)
	if formatted3 != input3 {
		t.Errorf("expected summary line unchanged, got: %q", formatted3)
	}
}

func TestCheckEdgeUpdateCasual(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "edge-version.json")

	var serverHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EdgeVersionPayload{
			Version:     "v0.2.0",
			PublishedAt: time.Now().UTC(),
			Highlights:  []string{"Highlight 1", "Highlight 2"},
			Status:      "ok",
		})
	}))
	defer server.Close()

	// 1. Initial call (cache miss) -> hits server and writes cache
	res1, err := CheckEdgeUpdateCasual("v0.1.0", cachePath, 1*time.Hour, server.URL)
	if err != nil {
		t.Fatalf("unexpected error on first casual check: %v", err)
	}
	if serverHits != 1 {
		t.Errorf("expected 1 server hit, got %d", serverHits)
	}
	if !res1.HasUpdate || res1.LatestVersion != "v0.2.0" {
		t.Errorf("expected HasUpdate true and latest v0.2.0, got: %+v", res1)
	}

	// 2. Second call within TTL (1 hour) -> hits cache, does not hit server
	res2, err := CheckEdgeUpdateCasual("v0.1.0", cachePath, 1*time.Hour, server.URL)
	if err != nil {
		t.Fatalf("unexpected error on second casual check: %v", err)
	}
	if serverHits != 1 {
		t.Errorf("expected serverHits to remain 1 on cache hit, got %d", serverHits)
	}
	if !res2.HasUpdate || res2.LatestVersion != "v0.2.0" {
		t.Errorf("expected cached update, got: %+v", res2)
	}
}
