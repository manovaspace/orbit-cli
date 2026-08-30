package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/manovaspace/orbit-cli/pkg/serverstore"
)

// DB wraps a SQLite database handle with serverstore repository access.
type DB struct {
	db   *sql.DB
	path string
}

// Ensure DB implements serverstore.Store.
var _ serverstore.Store = (*DB)(nil)

// NewDB opens or creates a SQLite database at the specified path, applies performance & integrity
// pragmas, and runs all pending schema migrations.
func NewDB(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite database path cannot be empty")
	}

	// Create directory hierarchy for file-based database paths if needed.
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0700); err != nil {
				return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
			}
		}
	}

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// In-memory databases must be limited to 1 connection to share schema and tables across queries.
	if path == ":memory:" || strings.Contains(path, "mode=memory") {
		sqlDB.SetMaxOpenConns(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Apply essential SQLite pragmas for WAL mode, busy timeout, and foreign keys.
	pragmas := []string{
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	}
	if path != ":memory:" && !strings.Contains(path, "mode=memory") {
		pragmas = append([]string{"PRAGMA journal_mode = WAL;"}, pragmas...)
	}

	for _, p := range pragmas {
		if _, err := sqlDB.ExecContext(ctx, p); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("failed to apply pragma %q: %w", p, err)
		}
	}

	// Run schema migrations
	if err := runMigrations(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("failed to run sqlite schema migrations: %w", err)
	}

	return &DB{
		db:   sqlDB,
		path: path,
	}, nil
}

// NewTestDB creates an in-memory SQLite database initialized with schema migrations for testing.
// It automatically registers database cleanup on test completion.
func NewTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create test sqlite database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

// SQLDB returns the underlying *sql.DB handle.
func (d *DB) SQLDB() *sql.DB {
	return d.db
}

// Ping verifies a connection to the database is still alive.
func (d *DB) Ping() error {
	return d.db.Ping()
}

// PingContext verifies a connection to the database is still alive with context.
func (d *DB) PingContext(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Path returns the configured database file path.
func (d *DB) Path() string {
	return d.path
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// Invites returns the InviteStore repository implementation.
func (d *DB) Invites() serverstore.InviteStore {
	return NewInviteStore(d.db)
}

// Challenges returns the ChallengeStore repository implementation.
func (d *DB) Challenges() serverstore.ChallengeStore {
	return NewChallengeStore(d.db)
}

// Grants returns the GrantStore repository implementation.
func (d *DB) Grants() serverstore.GrantStore {
	return NewGrantStore(d.db)
}

// RateLimits returns the RateLimitStore repository implementation.
func (d *DB) RateLimits() serverstore.RateLimitStore {
	return nil
}
