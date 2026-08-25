package user

import "time"

type UserStatus string

const (
	StatusActive   UserStatus = "active"
	StatusLocked   UserStatus = "locked"
	StatusArchived UserStatus = "archived"
)

// DeveloperUser represents a provisioned team member across the platform.
type DeveloperUser struct {
	UID             string     `json:"uid"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Role            string     `json:"role"` // admin, developer, readonly
	Status          UserStatus `json:"status"`
	WireGuardIP     string     `json:"wireguard_ip,omitempty"`
	WireGuardPubKey string     `json:"wireguard_pubkey,omitempty"`
	ForgejoUser     string     `json:"forgejo_username,omitempty"`
	SSHKeyCount     int        `json:"ssh_key_count"`
	LockReason      string     `json:"lock_reason,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	LastSeenAt      *time.Time `json:"last_seen_at,omitempty"`
}

// DeprovisionSummary records the result of an atomic offboarding operation.
type DeprovisionSummary struct {
	UID              string    `json:"uid"`
	Email            string    `json:"email"`
	LldapRemoved     bool      `json:"lldap_removed"`
	ForgejoRemoved   bool      `json:"forgejo_removed"`
	WireGuardFreedIP string    `json:"wireguard_freed_ip"`
	InvitesRevoked   int       `json:"invites_revoked"`
	CompletedAt      time.Time `json:"completed_at"`
}

// UserStore defines the persistent storage file schema (~/.manova/users.json).
type UserStore struct {
	Users     []DeveloperUser `json:"users"`
	UpdatedAt time.Time       `json:"updated_at"`
}
