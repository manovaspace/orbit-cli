package invite

import (
	"errors"
	"time"
)

var (
	ErrInvalidToken     = errors.New("invalid invite token")
	ErrTokenExpired     = errors.New("invite token has expired")
	ErrInvalidSignature = errors.New("invalid token signature")
	ErrMalformedToken   = errors.New("malformed invite token")
	ErrInviteNotFound   = errors.New("invitation not found")
)

type InviteRequest struct {
	Email       string            `json:"email"`
	DisplayName string            `json:"display_name"`
	Scope       string            `json:"scope"`
	TTL         time.Duration     `json:"ttl,omitempty"`
	CreatedBy   string            `json:"created_by,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type InviteClaims struct {
	ID          string            `json:"id"`
	Email       string            `json:"email"`
	DisplayName string            `json:"display_name"`
	Scope       string            `json:"scope"`
	IssuedAt    time.Time         `json:"iat"`
	ExpiresAt   time.Time         `json:"exp"`
	CreatedBy   string            `json:"created_by,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (c *InviteClaims) IsExpired() bool {
	if c == nil || c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().UTC().After(c.ExpiresAt)
}

type InviteRecord struct {
	ID          string            `json:"id"`
	Email       string            `json:"email"`
	DisplayName string            `json:"display_name,omitempty"`
	Scope       string            `json:"scope"`
	Token       string            `json:"token"`
	Revoked     bool              `json:"revoked"`
	RevokedAt   *time.Time        `json:"revoked_at,omitempty"`
	IssuedAt    time.Time         `json:"issued_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	CreatedBy   string            `json:"created_by,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (r *InviteRecord) Status() string {
	if r.Revoked {
		return "revoked"
	}
	if !r.ExpiresAt.IsZero() && time.Now().UTC().After(r.ExpiresAt) {
		return "expired"
	}
	return "active"
}
