package updater

import "time"

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
