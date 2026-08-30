package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/serverstore"
)

// InviteStore implements serverstore.InviteStore backed by SQLite.
type InviteStore struct {
	db *sql.DB
}

// NewInviteStore creates a new SQLite-backed InviteStore.
func NewInviteStore(db *sql.DB) *InviteStore {
	return &InviteStore{db: db}
}

// Ensure InviteStore implements serverstore.InviteStore.
var _ serverstore.InviteStore = (*InviteStore)(nil)

// SaveInvite inserts or updates an invitation record in SQLite.
func (s *InviteStore) SaveInvite(ctx context.Context, rec *invite.InviteRecord) error {
	if rec == nil {
		return errors.New("cannot save nil invite record")
	}

	createdAt := rec.IssuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	expiresAt := rec.ExpiresAt.UTC()

	var revokedAt *time.Time
	if rec.Revoked {
		if rec.RevokedAt != nil {
			t := rec.RevokedAt.UTC()
			revokedAt = &t
		} else {
			t := time.Now().UTC()
			revokedAt = &t
		}
	}

	var metadataJSON *string
	if len(rec.Metadata) > 0 {
		data, err := json.Marshal(rec.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal invite metadata: %w", err)
		}
		str := string(data)
		metadataJSON = &str
	}

	query := `
INSERT INTO invites (
    id, token, email, display_name, scope, created_by, revoked, revoked_at, created_at, expires_at, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    token = excluded.token,
    email = excluded.email,
    display_name = excluded.display_name,
    scope = excluded.scope,
    created_by = excluded.created_by,
    revoked = excluded.revoked,
    revoked_at = excluded.revoked_at,
    created_at = excluded.created_at,
    expires_at = excluded.expires_at,
    metadata_json = excluded.metadata_json;`

	_, err := s.db.ExecContext(ctx, query,
		rec.ID,
		rec.Token,
		rec.Email,
		rec.DisplayName,
		rec.Scope,
		rec.CreatedBy,
		rec.Revoked,
		revokedAt,
		createdAt,
		expiresAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to save invite %s: %w", rec.ID, err)
	}

	return nil
}

// GetInvite finds an invitation by exact token, exact ID, or unique ID prefix.
func (s *InviteStore) GetInvite(ctx context.Context, tokenOrID string) (*invite.InviteRecord, error) {
	tokenOrID = strings.TrimSpace(tokenOrID)
	if tokenOrID == "" {
		return nil, invite.ErrInviteNotFound
	}

	// 1. Exact match on Token or ID
	exactQuery := `
SELECT id, token, email, display_name, scope, created_by, revoked, revoked_at, created_at, expires_at, metadata_json
FROM invites
WHERE token = ? OR id = ?
LIMIT 1;`

	row := s.db.QueryRowContext(ctx, exactQuery, tokenOrID, tokenOrID)
	rec, err := scanInviteRow(row)
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to query invite by token/id: %w", err)
	}

	// 2. Prefix match on ID
	prefixQuery := `
SELECT id, token, email, display_name, scope, created_by, revoked, revoked_at, created_at, expires_at, metadata_json
FROM invites
WHERE id LIKE ? || '%'
LIMIT 2;`

	rows, err := s.db.QueryContext(ctx, prefixQuery, tokenOrID)
	if err != nil {
		return nil, fmt.Errorf("failed to query invite by prefix: %w", err)
	}
	defer rows.Close()

	var matches []*invite.InviteRecord
	for rows.Next() {
		r, err := scanInviteRows(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan prefix match row: %w", err)
		}
		matches = append(matches, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating prefix match rows: %w", err)
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous invite ID prefix %q matches multiple invitations", tokenOrID)
	}

	return nil, invite.ErrInviteNotFound
}

// ListInvites returns all invitations ordered by created_at DESC.
func (s *InviteStore) ListInvites(ctx context.Context) ([]*invite.InviteRecord, error) {
	query := `
SELECT id, token, email, display_name, scope, created_by, revoked, revoked_at, created_at, expires_at, metadata_json
FROM invites
ORDER BY created_at DESC;`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list invites: %w", err)
	}
	defer rows.Close()

	records := make([]*invite.InviteRecord, 0)
	for rows.Next() {
		rec, err := scanInviteRows(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan invite record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating invite rows: %w", err)
	}

	return records, nil
}

// RevokeInvite marks an invitation as revoked and updates revoked_at.
func (s *InviteStore) RevokeInvite(ctx context.Context, tokenOrID string) (*invite.InviteRecord, error) {
	rec, err := s.GetInvite(ctx, tokenOrID)
	if err != nil {
		return nil, err
	}

	if rec.Revoked {
		return rec, nil
	}

	now := time.Now().UTC()
	rec.Revoked = true
	rec.RevokedAt = &now

	query := `UPDATE invites SET revoked = 1, revoked_at = ? WHERE id = ?;`
	if _, err := s.db.ExecContext(ctx, query, now, rec.ID); err != nil {
		return nil, fmt.Errorf("failed to revoke invite %s: %w", rec.ID, err)
	}

	return rec, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInviteRow(row *sql.Row) (*invite.InviteRecord, error) {
	return scanInvite(row)
}

func scanInviteRows(rows *sql.Rows) (*invite.InviteRecord, error) {
	return scanInvite(rows)
}

func scanInvite(s rowScanner) (*invite.InviteRecord, error) {
	var (
		rec          invite.InviteRecord
		displayName  sql.NullString
		createdBy    sql.NullString
		revokedAt    sql.NullTime
		createdAt    time.Time
		expiresAt    time.Time
		metadataJSON sql.NullString
	)

	err := s.Scan(
		&rec.ID,
		&rec.Token,
		&rec.Email,
		&displayName,
		&rec.Scope,
		&createdBy,
		&rec.Revoked,
		&revokedAt,
		&createdAt,
		&expiresAt,
		&metadataJSON,
	)
	if err != nil {
		return nil, err
	}

	if displayName.Valid {
		rec.DisplayName = displayName.String
	}
	if createdBy.Valid {
		rec.CreatedBy = createdBy.String
	}
	if revokedAt.Valid {
		t := revokedAt.Time.UTC()
		rec.RevokedAt = &t
	}

	rec.IssuedAt = createdAt.UTC()
	rec.ExpiresAt = expiresAt.UTC()

	if metadataJSON.Valid && len(strings.TrimSpace(metadataJSON.String)) > 0 {
		var meta map[string]string
		if err := json.Unmarshal([]byte(metadataJSON.String), &meta); err == nil {
			rec.Metadata = meta
		}
	}

	return &rec, nil
}
