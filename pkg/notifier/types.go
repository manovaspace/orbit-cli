package notifier

import "time"

const (
	DefaultFeedURL    = "https://get.manova.space/api/feed"
	DefaultFeedFile   = "~/.manova/feed.json"
	DefaultStoreFile  = "~/.manova/messages.json"
	MaxMessagesPerRun = 3
)

// FeedResponse is the /api/feed response schema.
type FeedResponse struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Status      string    `json:"status"`
	Messages    []Message `json:"messages"`
}

// Message is a single notifier message from the feed.
type Message struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`     // release|broadcast|tip|alert
	Priority    string     `json:"priority"` // critical|high|normal|low
	Title       string     `json:"title"`
	Body        string     `json:"body,omitempty"`
	Action      string     `json:"action,omitempty"`
	PublishedAt time.Time  `json:"published_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// IsExpired returns true if the message has a non-nil ExpiresAt in the past.
func (m Message) IsExpired() bool {
	return m.ExpiresAt != nil && time.Now().UTC().After(*m.ExpiresAt)
}

// IsCritical returns true for critical priority messages.
func (m Message) IsCritical() bool {
	return m.Priority == "critical"
}

// TypeIcon returns a display icon string for the message type.
func (m Message) TypeIcon() string {
	switch m.Type {
	case "release":
		return "🔔"
	case "broadcast":
		return "📢"
	case "tip":
		return "💡"
	case "alert":
		return "⚠"
	default:
		return "ℹ"
	}
}

// MessageStore is the client-side tracking file (~/.manova/messages.json).
type MessageStore struct {
	Seen      []string  `json:"seen"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FeedState is the cached feed persisted to ~/.manova/feed.json.
type FeedState struct {
	LatestVersion string    `json:"latest_version"`
	LastCheckedAt time.Time `json:"last_checked_at"`
	ServerStatus  string    `json:"server_status"`
	LastError     string    `json:"last_error,omitempty"`
	Messages      []Message `json:"messages"`
}
