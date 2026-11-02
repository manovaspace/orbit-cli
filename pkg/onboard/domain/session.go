package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// SessionState is the persisted checkpoint of one onboarding run.
type SessionState struct {
	ID              string            `json:"id"`
	Email           string            `json:"email"`
	DisplayName     string            `json:"display_name"`
	UID             string            `json:"uid"`
	CurrentStage    Stage             `json:"current_stage"`
	InviteToken     string            `json:"invite_token,omitempty"`
	SSHPublicKey    string            `json:"ssh_public_key,omitempty"`
	ForgejoToken    string            `json:"forgejo_token,omitempty"`
	WireGuardConfig string            `json:"wireguard_config,omitempty"`
	WorkspacePath   string            `json:"workspace_path,omitempty"`
	ClonedRepos     []string          `json:"cloned_repos,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// NewSession generates a session with an 8-byte hex ID and the init stage.
func NewSession(email, displayName string) *SessionState {
	rawID := make([]byte, 8)
	_, _ = rand.Read(rawID)
	now := time.Now().UTC()

	return &SessionState{
		ID:           hex.EncodeToString(rawID),
		Email:        email,
		DisplayName:  displayName,
		CurrentStage: StageInit,
		ClonedRepos:  make([]string, 0),
		Metadata:     make(map[string]string),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// SessionStore is the checkpoint port of the onboarding flow.
type SessionStore interface {
	HasPending() bool
	Load() (*SessionState, error)
	Save(*SessionState) error
	Clear() error
	Create(email, name string) *SessionState
}

// Pending reports whether the session has not reached the completed stage.
func (s *SessionState) Pending() bool {
	return s != nil && s.CurrentStage != StageCompleted && s.CurrentStage != StageComplete
}
