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

// GrantStore implements serverstore.GrantStore backed by SQLite.
type GrantStore struct {
	db *sql.DB
}

// NewGrantStore creates a new SQLite-backed GrantStore.
func NewGrantStore(db *sql.DB) *GrantStore {
	return &GrantStore{db: db}
}

// Ensure GrantStore implements serverstore.GrantStore and owner.GrantStore.
var (
	_ serverstore.GrantStore = (*GrantStore)(nil)
	_ owner.GrantStore       = (*GrantStore)(nil)
)

// SaveGrant inserts or updates an admin grant record in SQLite.
func (s *GrantStore) SaveGrant(ctx context.Context, g *owner.AdminGrant) error {
	if g == nil {
		return errors.New("cannot save nil grant")
	}

	normEmail := strings.ToLower(strings.TrimSpace(g.Email))
	if normEmail == "" {
		return errors.New("grant email cannot be empty")
	}
	g.Email = normEmail

	if g.ID == "" {
		rawID := make([]byte, 16)
		if _, err := rand.Read(rawID); err != nil {
			return fmt.Errorf("failed to generate grant ID: %w", err)
		}
		g.ID = hex.EncodeToString(rawID)
	}

	createdAt := g.IssuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
		g.IssuedAt = createdAt
	} else {
		createdAt = createdAt.UTC()
	}

	expiresAt := g.ExpiresAt.UTC()

	role := g.Role
	if role == "" {
		role = owner.DefaultGrantRole
		g.Role = role
	}

	var usedAt *time.Time
	if g.Used {
		if g.UsedAt != nil {
			t := g.UsedAt.UTC()
			usedAt = &t
		} else {
			t := time.Now().UTC()
			usedAt = &t
		}
	}

	query := `
INSERT INTO admin_grants (
    id, email, code_hash, salt, role, created_by, used, used_at, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    email = excluded.email,
    code_hash = excluded.code_hash,
    salt = excluded.salt,
    role = excluded.role,
    created_by = excluded.created_by,
    used = excluded.used,
    used_at = excluded.used_at,
    created_at = excluded.created_at,
    expires_at = excluded.expires_at;`

	_, err := s.db.ExecContext(ctx, query,
		g.ID,
		g.Email,
		g.CodeHash,
		g.Salt,
		role,
		g.CreatedBy,
		g.Used,
		usedAt,
		createdAt,
		expiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save grant %s: %w", g.ID, err)
	}

	return nil
}

// GetGrant finds an active or matching grant by email and optional code hash.
func (s *GrantStore) GetGrant(ctx context.Context, email, codeHash string) (*owner.AdminGrant, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return nil, owner.ErrGrantNotFound
	}

	now := time.Now().UTC()
	var (
		query string
		args  []any
	)

	if codeHash != "" {
		query = `
SELECT id, email, code_hash, salt, role, created_by, used, used_at, created_at, expires_at
FROM admin_grants
WHERE email = ? AND code_hash = ? AND used = 0 AND expires_at > ?
ORDER BY created_at DESC
LIMIT 1;`
		args = []any{normEmail, codeHash, now}
	} else {
		query = `
SELECT id, email, code_hash, salt, role, created_by, used, used_at, created_at, expires_at
FROM admin_grants
WHERE email = ? AND used = 0 AND expires_at > ?
ORDER BY created_at DESC
LIMIT 1;`
		args = []any{normEmail, now}
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	g, err := scanGrantRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, owner.ErrGrantNotFound
		}
		return nil, fmt.Errorf("failed to get admin grant: %w", err)
	}

	return g, nil
}

// MarkUsed marks a grant as used and sets the used timestamp.
func (s *GrantStore) MarkUsed(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return owner.ErrGrantNotFound
	}

	now := time.Now().UTC()
	query := `UPDATE admin_grants SET used = 1, used_at = ? WHERE id = ?;`
	res, err := s.db.ExecContext(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark grant used: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return owner.ErrGrantNotFound
	}

	return nil
}

// ListActiveGrants retrieves all active, unused, unexpired admin grants ordered by created_at DESC.
func (s *GrantStore) ListActiveGrants(ctx context.Context) ([]*owner.AdminGrant, error) {
	now := time.Now().UTC()
	query := `
SELECT id, email, code_hash, salt, role, created_by, used, used_at, created_at, expires_at
FROM admin_grants
WHERE used = 0 AND expires_at > ?
ORDER BY created_at DESC;`

	rows, err := s.db.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("failed to list active grants: %w", err)
	}
	defer rows.Close()

	grants := make([]*owner.AdminGrant, 0)
	for rows.Next() {
		g, err := scanGrantRows(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan grant record: %w", err)
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating grant rows: %w", err)
	}

	return grants, nil
}

func scanGrantRow(row *sql.Row) (*owner.AdminGrant, error) {
	return scanGrant(row)
}

func scanGrantRows(rows *sql.Rows) (*owner.AdminGrant, error) {
	return scanGrant(rows)
}

func scanGrant(s rowScanner) (*owner.AdminGrant, error) {
	var (
		g         owner.AdminGrant
		usedAt    sql.NullTime
		createdAt time.Time
		expiresAt time.Time
	)

	err := s.Scan(
		&g.ID,
		&g.Email,
		&g.CodeHash,
		&g.Salt,
		&g.Role,
		&g.CreatedBy,
		&g.Used,
		&usedAt,
		&createdAt,
		&expiresAt,
	)
	if err != nil {
		return nil, err
	}

	if usedAt.Valid {
		t := usedAt.Time.UTC()
		g.UsedAt = &t
	}
	g.IssuedAt = createdAt.UTC()
	g.ExpiresAt = expiresAt.UTC()
	g.MaxAttempts = owner.DefaultGrantMaxAttempts

	return &g, nil
}
