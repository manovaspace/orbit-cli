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
	// DefaultReleaseURL is the primary release repository base URL.
	DefaultReleaseURL = "https://api.github.com"
	// DefaultRepoSlug is the default repository slug for manova/orbit CLI releases.
	DefaultRepoSlug = "manovaspace/orbit-cli"
	// DefaultCacheFile is the default file path for caching update checks.
	DefaultCacheFile = "~/.orbit/update-check.json"
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

// IsNewerVersion returns true if target represents a newer semver version than current.
// Handles "v" prefixes and "dev" versions gracefully.
func IsNewerVersion(current, target string) bool {
	cur := strings.TrimSpace(current)
	tgt := strings.TrimSpace(target)

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
	if apiURL == "" {
		apiURL = DefaultReleaseURL
	}
	if repoSlug == "" {
		repoSlug = DefaultRepoSlug
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

// GetAuthToken resolves a GitHub API token from standard environment variables.
func GetAuthToken() string {
	for _, envKey := range []string{"GITHUB_TOKEN", "GH_TOKEN", "ORBIT_GITHUB_TOKEN"} {
		if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
			return val
		}
	}
	return ""
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
	if token := GetAuthToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

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
		ghCfg := selfupdate.GitHubConfig{}
		if token := GetAuthToken(); token != "" {
			ghCfg.APIToken = token
		}
		ghSource, err := selfupdate.NewGitHubSource(ghCfg)
		if err != nil {
			return fmt.Errorf("failed to create default github source: %w", err)
		}
		source = ghSource
	}

	// Explicitly configure filters so that regardless of whether the executable
	// was invoked via symlink "o" or "m", or "orbit" or "manova", it matches release assets.
	cfg := selfupdate.Config{
		Source:  source,
		Filters: []string{"^orbit[-_]", "^manova[-_]"},
	}

	// Configure SHA-256 checksums.txt validator
	cfg.Validator = &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"}

	updater, err := selfupdate.NewUpdater(cfg)
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
		// If SHA-256 validation failed because release didn't publish checksums, fallback to unvalidated update
		if strings.Contains(strings.ToLower(err.Error()), "validation") || strings.Contains(strings.ToLower(err.Error()), "checksum") {
			fallbackUpdater, fErr := selfupdate.NewUpdater(selfupdate.Config{
				Source:  source,
				Filters: []string{"^orbit[-_]", "^manova[-_]"},
			})
			if fErr == nil {
				release, err = fallbackUpdater.UpdateSelf(context.Background(), verToCompare, repo)
			}
		}
		if err != nil {
			return fmt.Errorf("self-update failed: %w", err)
		}
	}

	if release == nil {
		// Already up to date
		return nil
	}

	return nil
}
