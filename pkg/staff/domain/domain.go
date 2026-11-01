// Package domain holds the staff directory types and rules that need no I/O.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// Member is a staff directory record.
type Member struct {
	UID             string   `json:"uid"`
	DisplayName     string   `json:"display_name"`
	FirstName       string   `json:"first_name"`
	LastName        string   `json:"last_name"`
	Mail            string   `json:"mail"`
	PersonalForward string   `json:"personal_forward"`
	Groups          []string `json:"groups"`
	Status          string   `json:"status"`
}

// CreateInput is the create request body plus optional idempotency key.
type CreateInput struct {
	UID             string   `json:"uid"`
	DisplayName     string   `json:"display_name"`
	FirstName       string   `json:"first_name,omitempty"`
	LastName        string   `json:"last_name,omitempty"`
	PersonalForward string   `json:"personal_forward"`
	Groups          []string `json:"groups"`
	IdempotencyKey  string   `json:"-"`
}

// CreateResult is the create response including one-time secrets.
type CreateResult struct {
	Member
	LDAPPassword string `json:"ldap_password,omitempty"`
	Idempotent   bool   `json:"idempotent"`
}

// UpdateInput is the PATCH body.
type UpdateInput struct {
	DisplayName     string   `json:"display_name,omitempty"`
	FirstName       string   `json:"first_name,omitempty"`
	LastName        string   `json:"last_name,omitempty"`
	PersonalForward string   `json:"personal_forward,omitempty"`
	Groups          []string `json:"groups"`
}

// ResetResult holds the rotated LLDAP password used for staff SSO and mail.
type ResetResult struct {
	LDAPPassword string `json:"ldap_password"`
	Warning      string `json:"warning,omitempty"`
}

// SplitGroups parses a comma-separated group list.
func SplitGroups(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NewIdempotencyKey returns a random 32-character hexadecimal key.
func NewIdempotencyKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Invite carries the onboarding token generated after create.
type Invite struct {
	ID          string    `json:"id"`
	Token       string    `json:"token"`
	Email       string    `json:"email"`
	ExpiresAt   time.Time `json:"expires_at"`
	Dispatched  bool      `json:"dispatched"`
	DispatchErr string    `json:"dispatch_error,omitempty"`
}
