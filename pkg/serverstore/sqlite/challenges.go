package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/serverstore"
)

// ChallengeStore implements serverstore.ChallengeStore backed by SQLite.
type ChallengeStore struct {
	db *sql.DB
}

// NewChallengeStore creates a new SQLite-backed ChallengeStore.
func NewChallengeStore(db *sql.DB) *ChallengeStore {
	return &ChallengeStore{db: db}
}

// Ensure ChallengeStore implements serverstore.ChallengeStore and owner.ChallengeStore.
var (
	_ serverstore.ChallengeStore = (*ChallengeStore)(nil)
	_ owner.ChallengeStore       = (*ChallengeStore)(nil)
)

// SaveChallenge inserts or updates a challenge record in SQLite.
func (s *ChallengeStore) SaveChallenge(ctx context.Context, ch *owner.Challenge) error {
	if ch == nil {
		return errors.New("cannot save nil challenge")
	}

	normEmail := strings.ToLower(strings.TrimSpace(ch.Email))
	if normEmail == "" {
		return errors.New("challenge email cannot be empty")
	}
	ch.Email = normEmail

	if ch.ID == "" {
		rawID := make([]byte, 16)
		if _, err := rand.Read(rawID); err != nil {
			return fmt.Errorf("failed to generate challenge ID: %w", err)
		}
		ch.ID = hex.EncodeToString(rawID)
	}

	createdAt := ch.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
		ch.CreatedAt = createdAt
	} else {
		createdAt = createdAt.UTC()
	}

	expiresAt := ch.ExpiresAt.UTC()

	maxAttempts := ch.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = owner.DefaultMaxAttempts
		ch.MaxAttempts = maxAttempts
	}

	var verifiedAt *time.Time
	if ch.Verified {
		if ch.VerifiedAt != nil {
			t := ch.VerifiedAt.UTC()
			verifiedAt = &t
		} else {
			t := time.Now().UTC()
			verifiedAt = &t
		}
	}

	query := `
INSERT INTO challenges (
    id, email, otp_hash, salt, attempts_count, max_attempts, verified, verified_at, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    email = excluded.email,
    otp_hash = excluded.otp_hash,
    salt = excluded.salt,
    attempts_count = excluded.attempts_count,
    max_attempts = excluded.max_attempts,
    verified = excluded.verified,
    verified_at = excluded.verified_at,
    created_at = excluded.created_at,
    expires_at = excluded.expires_at;`

	_, err := s.db.ExecContext(ctx, query,
		ch.ID,
		ch.Email,
		ch.CodeHash,
		ch.Salt,
		ch.Attempts,
		maxAttempts,
		ch.Verified,
		verifiedAt,
		createdAt,
		expiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save challenge %s: %w", ch.ID, err)
	}

	return nil
}

// GetActiveChallenge retrieves the most recent active unverified challenge for an email.
func (s *ChallengeStore) GetActiveChallenge(ctx context.Context, email string) (*owner.Challenge, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return nil, owner.ErrChallengeNotFound
	}

	now := time.Now().UTC()
	query := `
SELECT id, email, otp_hash, salt, attempts_count, max_attempts, verified, verified_at, created_at, expires_at
FROM challenges
WHERE email = ? AND verified = 0 AND expires_at > ?
ORDER BY created_at DESC
LIMIT 1;`

	row := s.db.QueryRowContext(ctx, query, normEmail, now)
	ch, err := scanChallengeRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, owner.ErrChallengeNotFound
		}
		return nil, fmt.Errorf("failed to get active challenge: %w", err)
	}

	return ch, nil
}

// IncrementAttempts increments the failed attempt count for the specified challenge ID.
func (s *ChallengeStore) IncrementAttempts(ctx context.Context, id string) (int, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, owner.ErrChallengeNotFound
	}

	query := `UPDATE challenges SET attempts_count = attempts_count + 1 WHERE id = ? RETURNING attempts_count;`
	var newAttempts int
	err := s.db.QueryRowContext(ctx, query, id).Scan(&newAttempts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, owner.ErrChallengeNotFound
		}
		return 0, fmt.Errorf("failed to increment challenge attempts: %w", err)
	}

	return newAttempts, nil
}

// MarkVerified marks a challenge as verified and sets the verified timestamp.
func (s *ChallengeStore) MarkVerified(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return owner.ErrChallengeNotFound
	}

	now := time.Now().UTC()
	query := `UPDATE challenges SET verified = 1, verified_at = ? WHERE id = ?;`
	res, err := s.db.ExecContext(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark challenge verified: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return owner.ErrChallengeNotFound
	}

	return nil
}

// PruneExpired removes all unverified challenges whose expiration time has passed.
func (s *ChallengeStore) PruneExpired(ctx context.Context) error {
	now := time.Now().UTC()
	query := `DELETE FROM challenges WHERE verified = 0 AND expires_at <= ?;`
	if _, err := s.db.ExecContext(ctx, query, now); err != nil {
		return fmt.Errorf("failed to prune expired challenges: %w", err)
	}
	return nil
}

func scanChallengeRow(row *sql.Row) (*owner.Challenge, error) {
	return scanChallenge(row)
}

func scanChallenge(s rowScanner) (*owner.Challenge, error) {
	var (
		ch         owner.Challenge
		verifiedAt sql.NullTime
		createdAt  time.Time
		expiresAt  time.Time
	)

	err := s.Scan(
		&ch.ID,
		&ch.Email,
		&ch.CodeHash,
		&ch.Salt,
		&ch.Attempts,
		&ch.MaxAttempts,
		&ch.Verified,
		&verifiedAt,
		&createdAt,
		&expiresAt,
	)
	if err != nil {
		return nil, err
	}

	if verifiedAt.Valid {
		t := verifiedAt.Time.UTC()
		ch.VerifiedAt = &t
	}
	ch.CreatedAt = createdAt.UTC()
	ch.ExpiresAt = expiresAt.UTC()

	return &ch, nil
}
