package worker

import "time"

const (
	// DefaultEdgeURL is the primary Cloudflare Edge live ping endpoint for version checks.
	DefaultEdgeURL = "https://get.manova.space/version"

	// DefaultStateFile is the default local file path storing edge version state.
	DefaultStateFile = "~/.manova/edge-version.json"

	// DefaultPollInterval is the worker polling frequency (1 minute).
	DefaultPollInterval = 1 * time.Minute

	// DefaultStaleThreshold is the staleness threshold after which CLI triggers self-healing (5 minutes).
	DefaultStaleThreshold = 5 * time.Minute
)

// EdgeVersionResponse matches the Cloudflare /version response schema.
type EdgeVersionResponse struct {
	Version             string    `json:"version"`
	TagName             string    `json:"tag_name"`
	PublishedAt         time.Time `json:"published_at"`
	Highlights          []string  `json:"highlights"`
	DownloadURLTemplate string    `json:"download_url_template"`
	Status              string    `json:"status"`
}

// EdgeVersionState represents the client-side state persisted to disk.
type EdgeVersionState struct {
	LatestVersion string    `json:"latest_version"`
	LastCheckedAt time.Time `json:"last_checked_at"`
	ServerStatus  string    `json:"server_status"`
	LastError     string    `json:"last_error,omitempty"`
	WorkerStatus  string    `json:"worker_status,omitempty"`
	WorkerPID     int       `json:"worker_pid,omitempty"`
	WorkerMode    string    `json:"worker_mode,omitempty"`
	Highlights    []string  `json:"highlights,omitempty"`
}
