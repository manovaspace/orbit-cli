package changelog

import "time"

// ReleaseEntry represents a single version release with its published date and highlights.
type ReleaseEntry struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Highlights  []string  `json:"highlights"`
	Body        string    `json:"body,omitempty"`
}

// ChangelogFeed represents the changelog history.
type ChangelogFeed struct {
	Releases []ReleaseEntry `json:"releases"`
}
