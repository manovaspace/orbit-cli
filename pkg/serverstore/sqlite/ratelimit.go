package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/serverstore"
)

// RateLimitStore implements serverstore.RateLimitStore backed by SQLite.
type RateLimitStore struct {
	db *sql.DB
}

// NewRateLimitStore creates a new SQLite-backed RateLimitStore.
func NewRateLimitStore(db *sql.DB) *RateLimitStore {
	return &RateLimitStore{db: db}
}

// Ensure RateLimitStore implements serverstore.RateLimitStore.
var _ serverstore.RateLimitStore = (*RateLimitStore)(nil)

// RecordEvent records a rate limit event timestamp for a given key and route.
func (s *RateLimitStore) RecordEvent(ctx context.Context, key, route string, ts time.Time) error {
	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}

	query := `INSERT INTO rate_limit_events (key, route, timestamp) VALUES (?, ?, ?);`
	if _, err := s.db.ExecContext(ctx, query, key, route, ts.UnixMilli()); err != nil {
		return fmt.Errorf("failed to record rate limit event: %w", err)
	}

	return nil
}

// CountEventsSince returns the number of recorded events for a given key and route since the given timestamp.
func (s *RateLimitStore) CountEventsSince(ctx context.Context, key, route string, since time.Time) (int, error) {
	query := `SELECT COUNT(*) FROM rate_limit_events WHERE key = ? AND route = ? AND timestamp >= ?;`
	var count int
	err := s.db.QueryRowContext(ctx, query, key, route, since.UTC().UnixMilli()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rate limit events: %w", err)
	}
	return count, nil
}

// PruneEventsOlderThan deletes all rate limit events timestamped before the cutoff time.
func (s *RateLimitStore) PruneEventsOlderThan(ctx context.Context, cutoff time.Time) error {
	query := `DELETE FROM rate_limit_events WHERE timestamp < ?;`
	if _, err := s.db.ExecContext(ctx, query, cutoff.UTC().UnixMilli()); err != nil {
		return fmt.Errorf("failed to prune rate limit events: %w", err)
	}
	return nil
}
