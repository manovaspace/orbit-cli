// Package domain holds the updater types and pure version rules.
package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	// DefaultReleaseURL is the primary release repository base URL.
	DefaultReleaseURL = "https://api.github.com"
	// DefaultRepoSlug is the default repository slug for Orbit CLI releases.
	DefaultRepoSlug = "manovaspace/orbit-cli"
	// DefaultCacheFile is the default file path for caching update checks.
	DefaultCacheFile = "~/.orbit/update-check.json"
	// DefaultTTL is the default cache validity duration (24 hours).
	DefaultTTL = 24 * time.Hour
)

// ReleaseInfo contains metadata about a published release.
type ReleaseInfo struct {
	Version      string    `json:"version"`
	TagName      string    `json:"tag_name"`
	AssetURL     string    `json:"asset_url"`
	ReleaseNotes string    `json:"release_notes"`
	PublishedAt  time.Time `json:"published_at"`
}

// UpdateCheckResult represents the outcome of checking for available updates.
type UpdateCheckResult struct {
	CurrentVersion string       `json:"current_version"`
	LatestVersion  string       `json:"latest_version"`
	HasUpdate      bool         `json:"has_update"`
	Release        *ReleaseInfo `json:"release,omitempty"`
	CheckedAt      time.Time    `json:"checked_at,omitempty"`
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

// ExtractReleaseHighlights parses markdown release notes body and returns up to maxCount
// clean summary bullet points for CLI display.
func ExtractReleaseHighlights(body string, maxCount int) []string {
	if maxCount <= 0 {
		maxCount = 5
	}
	var highlights []string
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "---") {
			continue
		}

		var item string
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			item = strings.TrimSpace(trimmed[2:])
		} else if strings.HasPrefix(trimmed, "• ") {
			item = strings.TrimSpace(strings.TrimPrefix(trimmed, "• "))
		}

		if item != "" {
			// Clean up markdown formatting for clean terminal rendering
			item = strings.ReplaceAll(item, "**", "")
			item = strings.ReplaceAll(item, "__", "")
			item = strings.ReplaceAll(item, "`", "")
			highlights = append(highlights, item)
			if len(highlights) >= maxCount {
				break
			}
		}
	}

	// Fallback if no bullet items found: extract first non-header paragraphs
	if len(highlights) == 0 {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "```") {
				continue
			}
			clean := strings.ReplaceAll(trimmed, "**", "")
			clean = strings.ReplaceAll(clean, "__", "")
			clean = strings.ReplaceAll(clean, "`", "")
			highlights = append(highlights, clean)
			if len(highlights) >= maxCount {
				break
			}
		}
	}

	return highlights
}
