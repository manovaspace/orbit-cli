package domain

import "time"

// Progress statuses reported for a stage.
const (
	StatusStarted    = "started"
	StatusInProgress = "in_progress"
	StatusResumed    = "resumed"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusSkipped    = "skipped"
)

// ProgressEvent is one structured progress notification emitted by the flow.
// Its JSON shape is the object the CLI emitted before the envelope existed.
type ProgressEvent struct {
	Stage     Stage     `json:"stage"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Details   any       `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
