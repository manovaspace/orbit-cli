package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
)

const (
	// DefaultGitHubURL is the primary public release repository base URL.
	DefaultGitHubURL = "https://api.github.com"
	// DefaultForgejoURL is the internal mirror repository base URL.
	DefaultForgejoURL = "https://git.dev.manova.space"
	// DefaultRepoSlug is the default repository slug for manova CLI releases.
	DefaultRepoSlug = "manovaspace/orbit-cli"
	// DefaultCacheFile is the default file path for caching update checks.
	DefaultCacheFile = "~/.manova/update-check.json"
	// DefaultTTL is the default cache validity duration (24 hours).
	DefaultTTL = 24 * time.Hour
)

type apiAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type apiRelease struct {
	ID          int64      `json:"id"`
	TagName     string     `json:"tag_name"`
	Name        string     `json:"name"`
	Body        string     `json:"body"`
	HTMLURL     string     `json:"html_url"`
	URL         string     `json:"url"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt time.Time  `json:"published_at"`
	Assets      []apiAsset `json:"assets"`
}

// FormatVersion normalizes a version string for display (e.g. "0.1.5" -> "v0.1.5", "dev" -> "dev").
func FormatVersion(v string) string {
	clean := strings.TrimSpace(v)
	if clean == "" || strings.EqualFold(clean, "dev") || strings.EqualFold(clean, "vdev") {
		return "dev"
	}
	for strings.HasPrefix(clean, "v") || strings.HasPrefix(clean, "V") {
		clean = clean[1:]
	}
	if strings.EqualFold(clean, "dev") {
		return "dev"
	}
	return "v" + clean
}

// TruncateReleaseNotes extracts up to maxItems top bullet points / change summaries from raw markdown notes.
// If more items exist, it appends a compact truncated summary line (e.g. "… (+N more changes)").
func TruncateReleaseNotes(notes string, maxItems int) []string {
	if maxItems <= 0 {
		maxItems = 5
	}

	lines := strings.Split(notes, "\n")
	var items []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "===") {
			continue
		}

		// Clean leading bullet characters (*, -, •, +, 1., 2.)
		cleaned := trimmed
		cleaned = strings.TrimPrefix(cleaned, "* ")
		cleaned = strings.TrimPrefix(cleaned, "- ")
		cleaned = strings.TrimPrefix(cleaned, "• ")
		cleaned = strings.TrimPrefix(cleaned, "+ ")
		if len(cleaned) > 3 && cleaned[0] >= '0' && cleaned[0] <= '9' && (cleaned[1] == '.' || cleaned[2] == '.') {
			idx := strings.Index(cleaned, ". ")
			if idx != -1 && idx < 4 {
				cleaned = strings.TrimSpace(cleaned[idx+2:])
			}
		}

		cleaned = strings.TrimSpace(cleaned)
		if cleaned != "" {
			items = append(items, cleaned)
		}
	}

	if len(items) == 0 {
		return nil
	}

	if len(items) <= maxItems {
		return items
	}

	truncated := append([]string{}, items[:maxItems]...)
	remaining := len(items) - maxItems
	truncated = append(truncated, fmt.Sprintf("… (+%d more changes)", remaining))
	return truncated
}

// IsNewerVersion returns true if target represents a newer semver version than current.
// Handles "v" prefixes and "dev" versions gracefully.
func IsNewerVersion(current, target string) bool {
	cur := strings.TrimSpace(current)
	tgt := strings.TrimSpace(target)

	for strings.HasPrefix(cur, "v") || strings.HasPrefix(cur, "V") {
		cur = cur[1:]
	}
	for strings.HasPrefix(tgt, "v") || strings.HasPrefix(tgt, "V") {
		tgt = tgt[1:]
	}

	isCurDev := cur == "" || strings.EqualFold(cur, "dev") || strings.HasPrefix(strings.ToLower(cur), "dev")
	isTgtDev := tgt == "" || strings.EqualFold(tgt, "dev") || strings.HasPrefix(strings.ToLower(tgt), "dev")

	if isTgtDev {
		return false
	}

	if isCurDev {
		// If current is dev, any valid semver release is newer
		_, err := semver.NewVersion(tgt)
		return err == nil
	}

	curVer, errCur := semver.NewVersion(cur)
	tgtVer, errTgt := semver.NewVersion(tgt)

	if errCur != nil || errTgt != nil {
		return false
	}

	return tgtVer.GreaterThan(curVer)
}

// ResolveAPIEndpoint constructs the full release API URL based on apiURL and repoSlug.
func ResolveAPIEndpoint(apiURL, repoSlug string) string {
	if repoSlug == "" {
		repoSlug = DefaultRepoSlug
	}

	if apiURL == "" {
		return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repoSlug)
	}

	trimmed := strings.TrimSuffix(apiURL, "/")

	if strings.HasSuffix(trimmed, "/releases/latest") {
		return trimmed
	}

	if strings.Contains(trimmed, "github.com") {
		if trimmed == "https://github.com" || trimmed == "http://github.com" || trimmed == "github.com" {
			return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repoSlug)
		}
		if strings.Contains(trimmed, "api.github.com") {
			return fmt.Sprintf("%s/repos/%s/releases/latest", trimmed, repoSlug)
		}
		return fmt.Sprintf("%s/api/v3/repos/%s/releases/latest", trimmed, repoSlug)
	}

	if strings.HasSuffix(trimmed, "/api/v1") || strings.Contains(trimmed, "/api/v1/") {
		return fmt.Sprintf("%s/repos/%s/releases/latest", trimmed, repoSlug)
	}

	return fmt.Sprintf("%s/api/v1/repos/%s/releases/latest", trimmed, repoSlug)
}

// CheckUpdate queries the remote release API (Forgejo or GitHub) and determines
// if a newer release is available compared to currentVersion.
func CheckUpdate(currentVersion, apiURL, repoSlug string) (*UpdateCheckResult, error) {
	endpoint := ResolveAPIEndpoint(apiURL, repoSlug)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "manova-cli-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query release API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	var release apiRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode release JSON: %w", err)
	}

	tagName := strings.TrimSpace(release.TagName)
	if tagName == "" {
		tagName = strings.TrimSpace(release.Name)
	}
	version := strings.TrimPrefix(tagName, "v")

	var assetURL string
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Match OS and Arch in asset names
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, goos) && strings.Contains(name, goarch) {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	// Fallback: match OS only
	if assetURL == "" {
		for _, asset := range release.Assets {
			name := strings.ToLower(asset.Name)
			if strings.Contains(name, goos) {
				assetURL = asset.BrowserDownloadURL
				break
			}
		}
	}
	// Fallback: first asset
	if assetURL == "" && len(release.Assets) > 0 {
		assetURL = release.Assets[0].BrowserDownloadURL
	}
	// Fallback: release HTML URL or URL
	if assetURL == "" {
		if release.HTMLURL != "" {
			assetURL = release.HTMLURL
		} else {
			assetURL = release.URL
		}
	}

	pubAt := release.PublishedAt
	if pubAt.IsZero() {
		pubAt = release.CreatedAt
	}

	hasUpdate := IsNewerVersion(currentVersion, tagName)

	relInfo := &ReleaseInfo{
		Version:      version,
		TagName:      tagName,
		AssetURL:     assetURL,
		ReleaseNotes: release.Body,
		PublishedAt:  pubAt,
	}

	return &UpdateCheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  version,
		HasUpdate:      hasUpdate,
		Release:        relInfo,
		CheckedAt:      time.Now(),
	}, nil
}

// ExpandCachePath expands leading ~ to user's home directory.
func ExpandCachePath(path string) string {
	if path == "" {
		path = DefaultCacheFile
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

// CheckUpdateCached reads the timestamped cache file. If within TTL, returns
// the cached result without making network requests. Otherwise, queries the API and refreshes the cache.
func CheckUpdateCached(currentVersion, cacheFile string, ttl time.Duration, apiURL, repoSlug string) (*UpdateCheckResult, error) {
	expandedPath := ExpandCachePath(cacheFile)
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	// Try reading existing cache
	if data, err := os.ReadFile(expandedPath); err == nil {
		var cached UpdateCheckResult
		if err := json.Unmarshal(data, &cached); err == nil {
			if !cached.CheckedAt.IsZero() && time.Since(cached.CheckedAt) < ttl {
				cached.CurrentVersion = currentVersion
				cached.HasUpdate = IsNewerVersion(currentVersion, cached.LatestVersion)
				return &cached, nil
			}
		}
	}

	// Fetch fresh update result
	result, err := CheckUpdate(currentVersion, apiURL, repoSlug)
	if err != nil {
		return nil, err
	}

	result.CheckedAt = time.Now()

	// Write cache best-effort
	if err := os.MkdirAll(filepath.Dir(expandedPath), 0755); err == nil {
		if data, err := json.MarshalIndent(result, "", "  "); err == nil {
			_ = os.WriteFile(expandedPath, data, 0644)
		}
	}

	return result, nil
}

// SelfUpdate downloads the matching OS/arch binary using creativeprojects/go-selfupdate
// and atomically replaces the currently running executable.
func SelfUpdate(currentVersion, repoSlug string, customSource selfupdate.Source) error {
	if repoSlug == "" {
		repoSlug = DefaultRepoSlug
	}

	var source selfupdate.Source
	if customSource != nil {
		source = customSource
	} else {
		githubSource, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
		if err != nil {
			return fmt.Errorf("failed to create default github source: %w", err)
		}
		source = githubSource
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize updater: %w", err)
	}

	verToCompare := currentVersion
	if verToCompare == "dev" || verToCompare == "" {
		verToCompare = "0.0.0-dev"
	}

	repo := selfupdate.ParseSlug(repoSlug)
	release, err := updater.UpdateSelf(context.Background(), verToCompare, repo)
	if err != nil {
		return fmt.Errorf("self-update failed: %w", err)
	}

	if release == nil {
		// Already up to date
		return nil
	}

	return nil
}
