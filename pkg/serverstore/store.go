package serverstore

import (
	"context"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
)

// Store is the unified storage interface for orbit-server persistence.
type Store interface {
	Invites() InviteStore
	Challenges() ChallengeStore
	Grants() GrantStore
	RateLimits() RateLimitStore
	Close() error
}

// InviteStore defines persistence operations for onboarding invitations.
type InviteStore interface {
	SaveInvite(ctx context.Context, rec *invite.InviteRecord) error
	GetInvite(ctx context.Context, tokenOrID string) (*invite.InviteRecord, error)
	ListInvites(ctx context.Context) ([]*invite.InviteRecord, error)
	RevokeInvite(ctx context.Context, tokenOrID string) (*invite.InviteRecord, error)
}

// ChallengeStore defines persistence operations for out-of-band OTP verification challenges.
type ChallengeStore interface {
	SaveChallenge(ctx context.Context, ch *owner.Challenge) error
	GetActiveChallenge(ctx context.Context, email string) (*owner.Challenge, error)
	IncrementAttempts(ctx context.Context, id string) (int, error)
	MarkVerified(ctx context.Context, id string) error
	PruneExpired(ctx context.Context) error
}

// GrantStore defines persistence operations for owner-issued 8-digit admin grants.
type GrantStore interface {
	SaveGrant(ctx context.Context, g *owner.AdminGrant) error
	GetGrant(ctx context.Context, email, codeHash string) (*owner.AdminGrant, error)
	MarkUsed(ctx context.Context, id string) error
	ListActiveGrants(ctx context.Context) ([]*owner.AdminGrant, error)
}

// RateLimitStore defines persistence operations for sliding-window rate limit tracking.
type RateLimitStore interface {
	RecordEvent(ctx context.Context, key, route string, ts time.Time) error
	CountEventsSince(ctx context.Context, key, route string, since time.Time) (int, error)
	PruneEventsOlderThan(ctx context.Context, cutoff time.Time) error
}
