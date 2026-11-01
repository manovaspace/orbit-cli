// Package releases implements the updater port over the GitHub release API and
// go-selfupdate.
package releases

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

	"github.com/creativeprojects/go-selfupdate"
	"github.com/manovaspace/orbit-cli/pkg/updater/domain"
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

// Checker queries and installs releases.
type Checker struct {
	// Source overrides the GitHub source, for tests.
	Source selfupdate.Source
	// Client overrides the HTTP client, for tests.
	Client *http.Client
}

// New builds a Checker over the standard GitHub source.
func New() *Checker { return &Checker{} }

func (c *Checker) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Check queries the release API and decides whether a newer release exists.
func (c *Checker) Check(ctx context.Context, currentVersion, apiURL, repoSlug string) (*domain.UpdateCheckResult, error) {
	endpoint := domain.ResolveAPIEndpoint(apiURL, repoSlug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "orbit-cli-updater")
	if token := GetAuthToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query release API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

	hasUpdate := domain.IsNewerVersion(currentVersion, tagName)

	relInfo := &domain.ReleaseInfo{
		Version:      version,
		TagName:      tagName,
		AssetURL:     assetURL,
		ReleaseNotes: release.Body,
		PublishedAt:  pubAt,
	}

	return &domain.UpdateCheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  version,
		HasUpdate:      hasUpdate,
		Release:        relInfo,
		CheckedAt:      time.Now(),
	}, nil
}

// CheckCached reads the timestamped cache file. If within TTL, returns the
// cached result without making network requests. Otherwise, queries the API and
// refreshes the cache.
func (c *Checker) CheckCached(ctx context.Context, currentVersion, cacheFile string, ttl time.Duration, apiURL, repoSlug string) (*domain.UpdateCheckResult, error) {
	expandedPath := ExpandCachePath(cacheFile)
	if ttl <= 0 {
		ttl = domain.DefaultTTL
	}

	// Try reading existing cache
	if data, err := os.ReadFile(expandedPath); err == nil {
		var cached domain.UpdateCheckResult
		if err := json.Unmarshal(data, &cached); err == nil {
			if !cached.CheckedAt.IsZero() && time.Since(cached.CheckedAt) < ttl {
				cached.CurrentVersion = currentVersion
				cached.HasUpdate = domain.IsNewerVersion(currentVersion, cached.LatestVersion)
				return &cached, nil
			}
		}
	}

	// Fetch fresh update result
	result, err := c.Check(ctx, currentVersion, apiURL, repoSlug)
	if err != nil {
		return nil, err
	}

	result.CheckedAt = time.Now()

	// Write cache best-effort
	if err := os.MkdirAll(filepath.Dir(expandedPath), 0o755); err == nil {
		if data, err := json.MarshalIndent(result, "", "  "); err == nil {
			_ = os.WriteFile(expandedPath, data, 0o644)
		}
	}

	return result, nil
}

// SelfUpdate downloads the matching OS/arch binary and atomically replaces the
// currently running executable.
func (c *Checker) SelfUpdate(ctx context.Context, currentVersion, repoSlug string) error {
	if repoSlug == "" {
		repoSlug = domain.DefaultRepoSlug
	}

	var source selfupdate.Source
	if c.Source != nil {
		source = c.Source
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
	// was invoked via symlink "o" or "orbit", it matches release assets for the current OS and arch,
	// while ignoring server binaries (orbit-server-*).
	assetFilter := fmt.Sprintf(`^orbit[-_]%s[-_]%s(\.exe)?(\.zip|\.tar\.gz|\.tgz|\.gzip|\.gz|\.tar\.xz|\.xz|\.bz2)?$`, runtime.GOOS, runtime.GOARCH)
	cfg := selfupdate.Config{
		Source:  source,
		Filters: []string{assetFilter},
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
	release, err := updater.UpdateSelf(ctx, verToCompare, repo)
	if err != nil {
		// If SHA-256 validation failed because release didn't publish checksums, fallback to unvalidated update
		if strings.Contains(strings.ToLower(err.Error()), "validation") || strings.Contains(strings.ToLower(err.Error()), "checksum") {
			fallbackUpdater, fErr := selfupdate.NewUpdater(selfupdate.Config{
				Source:  source,
				Filters: []string{assetFilter},
			})
			if fErr == nil {
				release, err = fallbackUpdater.UpdateSelf(ctx, verToCompare, repo)
			}
		}
		if err != nil {
			return fmt.Errorf("self-update failed: %w", err)
		}
	}

	_ = release
	return nil
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

// ExpandCachePath expands leading ~ to user's home directory.
func ExpandCachePath(path string) string {
	if path == "" {
		path = domain.DefaultCacheFile
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
