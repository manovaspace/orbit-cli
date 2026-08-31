package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestInvites_SaveAndGet(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(24 * time.Hour)

	rec := &invite.InviteRecord{
		ID:          "inv_test123456789",
		Token:       "tok_secret_token_123456",
		Email:       "user@example.com",
		DisplayName: "Test User",
		Scope:       "orbit:admin",
		CreatedBy:   "admin@example.com",
		IssuedAt:    now,
		ExpiresAt:   exp,
		Metadata: map[string]string{
			"team":    "platform",
			"tier":    "gold",
			"version": "1.0",
		},
	}

	if err := store.SaveInvite(ctx, rec); err != nil {
		t.Fatalf("SaveInvite failed: %v", err)
	}

	// 1. Get by exact ID
	byId, err := store.GetInvite(ctx, "inv_test123456789")
	if err != nil {
		t.Fatalf("GetInvite by exact ID failed: %v", err)
	}
	if byId.ID != rec.ID || byId.Email != rec.Email || byId.DisplayName != rec.DisplayName {
		t.Fatalf("GetInvite by exact ID mismatch: got %+v, want %+v", byId, rec)
	}
	if byId.Metadata["team"] != "platform" || byId.Metadata["tier"] != "gold" {
		t.Fatalf("Metadata roundtrip failed: got %+v", byId.Metadata)
	}
	if !byId.IssuedAt.Equal(now) || !byId.ExpiresAt.Equal(exp) {
		t.Fatalf("Timestamp mismatch: issuedAt=%v (want %v), expiresAt=%v (want %v)", byId.IssuedAt, now, byId.ExpiresAt, exp)
	}

	// 2. Get by exact token
	byToken, err := store.GetInvite(ctx, "tok_secret_token_123456")
	if err != nil {
		t.Fatalf("GetInvite by exact token failed: %v", err)
	}
	if byToken.ID != rec.ID {
		t.Fatalf("GetInvite by token mismatch: got ID %s, want %s", byToken.ID, rec.ID)
	}

	// 3. Get by ID prefix
	byPrefix, err := store.GetInvite(ctx, "inv_test12")
	if err != nil {
		t.Fatalf("GetInvite by prefix failed: %v", err)
	}
	if byPrefix.ID != rec.ID {
		t.Fatalf("GetInvite by prefix mismatch: got ID %s, want %s", byPrefix.ID, rec.ID)
	}

	// 4. Non-existent lookup
	_, err = store.GetInvite(ctx, "non_existent_token_or_id")
	if err == nil {
		t.Fatal("expected error for non-existent invite, got nil")
	}
	if err != invite.ErrInviteNotFound {
		t.Fatalf("expected ErrInviteNotFound, got %v", err)
	}
}

func TestInvites_PrefixAmbiguity(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)

	rec1 := &invite.InviteRecord{
		ID:        "inv_prefix_match_1",
		Token:     "tok_1",
		Email:     "u1@example.com",
		Scope:     "orbit:user",
		IssuedAt:  now,
		ExpiresAt: exp,
	}
	rec2 := &invite.InviteRecord{
		ID:        "inv_prefix_match_2",
		Token:     "tok_2",
		Email:     "u2@example.com",
		Scope:     "orbit:user",
		IssuedAt:  now,
		ExpiresAt: exp,
	}

	if err := store.SaveInvite(ctx, rec1); err != nil {
		t.Fatalf("SaveInvite rec1 failed: %v", err)
	}
	if err := store.SaveInvite(ctx, rec2); err != nil {
		t.Fatalf("SaveInvite rec2 failed: %v", err)
	}

	// Prefix that matches both rec1 and rec2 should return an ambiguity error
	_, err := store.GetInvite(ctx, "inv_prefix_match_")
	if err == nil {
		t.Fatal("expected ambiguity error for matching prefix, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected error to mention ambiguous, got %v", err)
	}

	// Full ID lookup should still work cleanly
	got1, err := store.GetInvite(ctx, "inv_prefix_match_1")
	if err != nil || got1.ID != "inv_prefix_match_1" {
		t.Fatalf("failed to fetch rec1 by exact ID: %v", err)
	}
}

func TestInvites_Upsert(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(24 * time.Hour)

	rec := &invite.InviteRecord{
		ID:          "inv_upsert_test",
		Token:       "tok_initial",
		Email:       "first@example.com",
		DisplayName: "First Name",
		Scope:       "orbit:user",
		IssuedAt:    now,
		ExpiresAt:   exp,
	}

	if err := store.SaveInvite(ctx, rec); err != nil {
		t.Fatalf("initial SaveInvite failed: %v", err)
	}

	// Update fields on the same ID
	rec.DisplayName = "Updated Name"
	rec.Email = "updated@example.com"
	rec.Metadata = map[string]string{"updated": "true"}

	if err := store.SaveInvite(ctx, rec); err != nil {
		t.Fatalf("upsert SaveInvite failed: %v", err)
	}

	fetched, err := store.GetInvite(ctx, "inv_upsert_test")
	if err != nil {
		t.Fatalf("GetInvite after upsert failed: %v", err)
	}
	if fetched.DisplayName != "Updated Name" || fetched.Email != "updated@example.com" {
		t.Fatalf("upsert data not updated: %+v", fetched)
	}
	if fetched.Metadata["updated"] != "true" {
		t.Fatalf("upsert metadata not updated: %+v", fetched.Metadata)
	}
}

func TestInvites_ListInvites(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	// Empty list check
	list, err := store.ListInvites(ctx)
	if err != nil {
		t.Fatalf("ListInvites on empty store failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 invites, got %d", len(list))
	}

	baseTime := time.Now().UTC().Truncate(time.Second)

	rec1 := &invite.InviteRecord{
		ID:        "inv_1",
		Token:     "tok_1",
		Email:     "a@example.com",
		Scope:     "orbit",
		IssuedAt:  baseTime.Add(-2 * time.Hour),
		ExpiresAt: baseTime.Add(24 * time.Hour),
	}
	rec2 := &invite.InviteRecord{
		ID:        "inv_2",
		Token:     "tok_2",
		Email:     "b@example.com",
		Scope:     "orbit",
		IssuedAt:  baseTime.Add(-1 * time.Hour),
		ExpiresAt: baseTime.Add(24 * time.Hour),
	}
	rec3 := &invite.InviteRecord{
		ID:        "inv_3",
		Token:     "tok_3",
		Email:     "c@example.com",
		Scope:     "orbit",
		IssuedAt:  baseTime,
		ExpiresAt: baseTime.Add(24 * time.Hour),
	}

	_ = store.SaveInvite(ctx, rec1)
	_ = store.SaveInvite(ctx, rec2)
	_ = store.SaveInvite(ctx, rec3)

	list, err = store.ListInvites(ctx)
	if err != nil {
		t.Fatalf("ListInvites failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 invites, got %d", len(list))
	}

	// Must be ordered by created_at DESC (rec3, rec2, rec1)
	if list[0].ID != "inv_3" || list[1].ID != "inv_2" || list[2].ID != "inv_1" {
		t.Fatalf("unexpected order: [%s, %s, %s]", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestInvites_RevokeInvite(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(24 * time.Hour)

	rec := &invite.InviteRecord{
		ID:        "inv_revoke_test_123",
		Token:     "tok_revoke_123",
		Email:     "revoke@example.com",
		Scope:     "orbit:user",
		IssuedAt:  now,
		ExpiresAt: exp,
		Revoked:   false,
	}

	if err := store.SaveInvite(ctx, rec); err != nil {
		t.Fatalf("SaveInvite failed: %v", err)
	}

	// Revoke by prefix
	revoked, err := store.RevokeInvite(ctx, "inv_revoke_test")
	if err != nil {
		t.Fatalf("RevokeInvite failed: %v", err)
	}
	if !revoked.Revoked {
		t.Fatal("expected revoked=true")
	}
	if revoked.RevokedAt == nil {
		t.Fatal("expected revoked_at to be non-nil")
	}

	// Fetch again to verify persistence
	fetched, err := store.GetInvite(ctx, "inv_revoke_test_123")
	if err != nil {
		t.Fatalf("GetInvite failed: %v", err)
	}
	if !fetched.Revoked || fetched.RevokedAt == nil {
		t.Fatalf("persisted invite is not revoked: %+v", fetched)
	}
	if fetched.Status() != "revoked" {
		t.Fatalf("expected status 'revoked', got %s", fetched.Status())
	}

	// Revoking an already revoked invite should return the revoked record without error
	revokedAgain, err := store.RevokeInvite(ctx, "inv_revoke_test_123")
	if err != nil {
		t.Fatalf("second RevokeInvite failed: %v", err)
	}
	if !revokedAgain.Revoked {
		t.Fatal("expected revoked=true on second revoke")
	}

	// Revoking non-existent should fail with ErrInviteNotFound
	_, err = store.RevokeInvite(ctx, "non_existent_prefix")
	if err != invite.ErrInviteNotFound {
		t.Fatalf("expected ErrInviteNotFound, got %v", err)
	}
}

func TestInvites_NilRecord(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	err := store.SaveInvite(ctx, nil)
	if err == nil {
		t.Fatal("expected error when saving nil invite record, got nil")
	}
}

func TestInvites_RevokeAll(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Invites()
	ctx := context.Background()

	// Empty store returns 0
	revoked, err := store.RevokeAll(ctx)
	if err != nil {
		t.Fatalf("RevokeAll on empty store failed: %v", err)
	}
	if len(revoked) != 0 {
		t.Fatalf("expected 0 revoked, got %d", len(revoked))
	}

	now := time.Now().UTC()
	_ = store.SaveInvite(ctx, &invite.InviteRecord{
		ID:        "inv_bulk_1",
		Token:     "tok_bulk_1",
		Email:     "dev1@example.com",
		Scope:     "core",
		IssuedAt:  now,
		ExpiresAt: now.Add(24 * time.Hour),
		Revoked:   false,
	})
	_ = store.SaveInvite(ctx, &invite.InviteRecord{
		ID:        "inv_bulk_2",
		Token:     "tok_bulk_2",
		Email:     "dev2@example.com",
		Scope:     "core",
		IssuedAt:  now,
		ExpiresAt: now.Add(48 * time.Hour),
		Revoked:   false,
	})
	_ = store.SaveInvite(ctx, &invite.InviteRecord{
		ID:        "inv_bulk_3",
		Token:     "tok_bulk_3",
		Email:     "dev3@example.com",
		Scope:     "core",
		IssuedAt:  now,
		ExpiresAt: now.Add(72 * time.Hour),
		Revoked:   true,
		RevokedAt: &now,
	})

	revoked, err = store.RevokeAll(ctx)
	if err != nil {
		t.Fatalf("RevokeAll failed: %v", err)
	}
	if len(revoked) != 2 {
		t.Fatalf("expected 2 newly revoked invites, got %d", len(revoked))
	}

	all, err := store.ListInvites(ctx)
	if err != nil {
		t.Fatalf("ListInvites failed: %v", err)
	}
	for _, r := range all {
		if !r.Revoked {
			t.Errorf("expected invite %s to be revoked", r.ID)
		}
	}
}

