package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestGrants_SaveAndGet(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Grants()
	ctx := context.Background()

	email := "Sara@Manova.Space"
	code := "8492-0194"
	salt := "test-grant-salt-456"
	codeHash := owner.HashGrantCode(code, salt)
	now := time.Now().UTC()

	g := &owner.AdminGrant{
		Email:       email,
		CodeHash:    codeHash,
		Salt:        salt,
		Role:        "admin",
		CreatedBy:   "root@manova.space",
		IssuedAt:    now,
		ExpiresAt:   now.Add(15 * time.Minute),
		MaxAttempts: 3,
		Used:        false,
	}

	if err := store.SaveGrant(ctx, g); err != nil {
		t.Fatalf("SaveGrant failed: %v", err)
	}

	if g.ID == "" {
		t.Errorf("expected SaveGrant to generate ID if empty")
	}

	// 1. GetGrant with email & matching codeHash
	retrieved, err := store.GetGrant(ctx, "sara@manova.space", codeHash)
	if err != nil {
		t.Fatalf("GetGrant (with hash) failed: %v", err)
	}
	if retrieved.ID != g.ID {
		t.Errorf("expected grant ID %s, got %s", g.ID, retrieved.ID)
	}
	if retrieved.Email != "sara@manova.space" {
		t.Errorf("expected normalized email sara@manova.space, got %s", retrieved.Email)
	}
	if retrieved.Role != "admin" {
		t.Errorf("expected role admin, got %s", retrieved.Role)
	}
	if retrieved.CreatedBy != "root@manova.space" {
		t.Errorf("expected created_by root@manova.space, got %s", retrieved.CreatedBy)
	}
	if retrieved.Used {
		t.Errorf("expected grant to be unused")
	}

	// 2. GetGrant with email only (empty codeHash)
	byEmail, err := store.GetGrant(ctx, "sara@manova.space", "")
	if err != nil {
		t.Fatalf("GetGrant (empty hash) failed: %v", err)
	}
	if byEmail.ID != g.ID {
		t.Errorf("expected grant ID %s, got %s", g.ID, byEmail.ID)
	}

	// 3. Non-matching codeHash
	_, err = store.GetGrant(ctx, "sara@manova.space", "wrong-hash")
	if !errors.Is(err, owner.ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantNotFound for non-matching hash, got %v", err)
	}

	// 4. Non-existent email
	_, err = store.GetGrant(ctx, "nonexistent@manova.space", "")
	if !errors.Is(err, owner.ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantNotFound for non-existent email, got %v", err)
	}

	// 5. Empty email
	_, err = store.GetGrant(ctx, "", "")
	if !errors.Is(err, owner.ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantNotFound for empty email, got %v", err)
	}
}

func TestGrants_MarkUsed(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Grants()
	ctx := context.Background()

	now := time.Now().UTC()
	g := &owner.AdminGrant{
		Email:     "burned@manova.space",
		CodeHash:  "hash-123",
		Salt:      "salt-123",
		Role:      "superadmin",
		CreatedBy: "root@manova.space",
		IssuedAt:  now,
		ExpiresAt: now.Add(15 * time.Minute),
	}

	if err := store.SaveGrant(ctx, g); err != nil {
		t.Fatalf("SaveGrant failed: %v", err)
	}

	if err := store.MarkUsed(ctx, g.ID); err != nil {
		t.Fatalf("MarkUsed failed: %v", err)
	}

	// Active lookup should return not found because used == 1
	_, err := store.GetGrant(ctx, "burned@manova.space", "")
	if !errors.Is(err, owner.ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantNotFound after MarkUsed, got %v", err)
	}

	// MarkUsed on non-existent ID
	err = store.MarkUsed(ctx, "non-existent-grant-id")
	if !errors.Is(err, owner.ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantNotFound for non-existent ID, got %v", err)
	}
}

func TestGrants_ListActiveGrants(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Grants()
	ctx := context.Background()

	past := time.Now().UTC().Add(-20 * time.Minute)
	now := time.Now().UTC()

	// 1. Expired grant
	expired := &owner.AdminGrant{
		ID:        "grant-expired",
		Email:     "expired@manova.space",
		CodeHash:  "hash-exp",
		Salt:      "salt-exp",
		Role:      "admin",
		CreatedBy: "root",
		IssuedAt:  past.Add(-10 * time.Minute),
		ExpiresAt: past,
	}
	if err := store.SaveGrant(ctx, expired); err != nil {
		t.Fatal(err)
	}

	// 2. Used grant
	usedTime := now.Add(-1 * time.Minute)
	used := &owner.AdminGrant{
		ID:        "grant-used",
		Email:     "used@manova.space",
		CodeHash:  "hash-used",
		Salt:      "salt-used",
		Role:      "admin",
		CreatedBy: "root",
		IssuedAt:  now.Add(-5 * time.Minute),
		ExpiresAt: now.Add(15 * time.Minute),
		Used:      true,
		UsedAt:    &usedTime,
	}
	if err := store.SaveGrant(ctx, used); err != nil {
		t.Fatal(err)
	}

	// 3. Active grant 1 (older)
	active1 := &owner.AdminGrant{
		ID:        "grant-active-1",
		Email:     "active1@manova.space",
		CodeHash:  "hash-act1",
		Salt:      "salt-act1",
		Role:      "maintainer",
		CreatedBy: "root",
		IssuedAt:  now.Add(-2 * time.Minute),
		ExpiresAt: now.Add(15 * time.Minute),
	}
	if err := store.SaveGrant(ctx, active1); err != nil {
		t.Fatal(err)
	}

	// 4. Active grant 2 (newer)
	active2 := &owner.AdminGrant{
		ID:        "grant-active-2",
		Email:     "active2@manova.space",
		CodeHash:  "hash-act2",
		Salt:      "salt-act2",
		Role:      "admin",
		CreatedBy: "root",
		IssuedAt:  now,
		ExpiresAt: now.Add(15 * time.Minute),
	}
	if err := store.SaveGrant(ctx, active2); err != nil {
		t.Fatal(err)
	}

	// List active grants
	activeList, err := store.ListActiveGrants(ctx)
	if err != nil {
		t.Fatalf("ListActiveGrants failed: %v", err)
	}

	if len(activeList) != 2 {
		t.Fatalf("expected 2 active grants, got %d", len(activeList))
	}

	// Verify descending order by created_at
	if activeList[0].ID != "grant-active-2" {
		t.Errorf("expected newest grant first ('grant-active-2'), got %s", activeList[0].ID)
	}
	if activeList[1].ID != "grant-active-1" {
		t.Errorf("expected older active grant second ('grant-active-1'), got %s", activeList[1].ID)
	}
}

func TestGrants_Validation(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Grants()
	ctx := context.Background()

	if err := store.SaveGrant(ctx, nil); err == nil {
		t.Errorf("expected error saving nil grant, got nil")
	}

	if err := store.SaveGrant(ctx, &owner.AdminGrant{Email: ""}); err == nil {
		t.Errorf("expected error saving grant with empty email, got nil")
	}
}
