package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/serverstore"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestDB_ImplementsStore(t *testing.T) {
	var _ serverstore.Store = (*sqlite.DB)(nil)
}

func TestNewDB_InMemory(t *testing.T) {
	db, err := sqlite.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory sqlite db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("db ping failed: %v", err)
	}

	// Verify all expected tables exist
	expectedTables := []string{
		"schema_migrations",
		"invites",
		"challenges",
		"admin_grants",
		"rate_limit_events",
	}

	for _, tbl := range expectedTables {
		var name string
		err := db.SQLDB().QueryRowContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", tbl, err)
		}
	}
}

func TestNewDB_FileBased(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "nested", "orbit.db")

	db, err := sqlite.NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create file-based sqlite db: %v", err)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected db file to exist on disk at %s", dbPath)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("db ping failed: %v", err)
	}

	// Verify Pragmas
	var foreignKeys int
	if err := db.SQLDB().QueryRow("PRAGMA foreign_keys;").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Errorf("expected PRAGMA foreign_keys = 1, got %d (err: %v)", foreignKeys, err)
	}

	var busyTimeout int
	if err := db.SQLDB().QueryRow("PRAGMA busy_timeout;").Scan(&busyTimeout); err != nil || busyTimeout != 5000 {
		t.Errorf("expected PRAGMA busy_timeout = 5000, got %d (err: %v)", busyTimeout, err)
	}

	var journalMode string
	if err := db.SQLDB().QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") {
		t.Errorf("expected PRAGMA journal_mode = wal, got %s (err: %v)", journalMode, err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}

	// Reopen to verify migration idempotency
	reopened, err := sqlite.NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen existing db: %v", err)
	}
	defer reopened.Close()

	if err := reopened.Ping(); err != nil {
		t.Fatalf("reopened db ping failed: %v", err)
	}
}

func TestNewTestDB(t *testing.T) {
	db := sqlite.NewTestDB(t)
	if db == nil {
		t.Fatal("expected NewTestDB to return non-nil DB")
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("NewTestDB ping failed: %v", err)
	}

	// Verify schema migration version 1 is recorded
	var count int
	err := db.SQLDB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected schema_migrations version 1 count=1, got count=%d (err: %v)", count, err)
	}
}

func TestNewDB_EmptyPath(t *testing.T) {
	_, err := sqlite.NewDB("")
	if err == nil {
		t.Fatal("expected error when opening db with empty path")
	}
}
