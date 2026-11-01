package domain

import (
	"context"
	"time"

	invitedomain "github.com/manovaspace/orbit-cli/pkg/invite/domain"
	ownerdomain "github.com/manovaspace/orbit-cli/pkg/owner/domain"
)

// Store is the persistence port of the onboarding edge daemon.
type Store interface {
	Invites() InviteStore
	Challenges() ChallengeStore
	Grants() GrantStore
	RateLimits() RateLimitStore
	Close() error
}

// InviteStore defines persistence operations for onboarding invitations.
type InviteStore interface {
	SaveInvite(ctx context.Context, rec *invitedomain.InviteRecord) error
	GetInvite(ctx context.Context, tokenOrID string) (*invitedomain.InviteRecord, error)
	ListInvites(ctx context.Context) ([]*invitedomain.InviteRecord, error)
	RevokeInvite(ctx context.Context, tokenOrID string) (*invitedomain.InviteRecord, error)
	RevokeAll(ctx context.Context) ([]*invitedomain.InviteRecord, error)
}

// ChallengeStore defines persistence operations for out-of-band OTP verification challenges.
type ChallengeStore interface {
	SaveChallenge(ctx context.Context, ch *ownerdomain.Challenge) error
	GetActiveChallenge(ctx context.Context, email string) (*ownerdomain.Challenge, error)
	IncrementAttempts(ctx context.Context, id string) (int, error)
	MarkVerified(ctx context.Context, id string) error
	PruneExpired(ctx context.Context) error
}

// GrantStore defines persistence operations for owner-issued 8-digit admin grants.
type GrantStore interface {
	SaveGrant(ctx context.Context, g *ownerdomain.AdminGrant) error
	GetGrant(ctx context.Context, email, codeHash string) (*ownerdomain.AdminGrant, error)
	MarkUsed(ctx context.Context, id string) error
	ListActiveGrants(ctx context.Context) ([]*ownerdomain.AdminGrant, error)
}

// RateLimitStore defines persistence operations for sliding-window rate limit tracking.
type RateLimitStore interface {
	RecordEvent(ctx context.Context, key, route string, ts time.Time) error
	CountEventsSince(ctx context.Context, key, route string, since time.Time) (int, error)
	PruneEventsOlderThan(ctx context.Context, cutoff time.Time) error
}
