package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestChallenges_SaveAndGetActive(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Challenges()
	ctx := context.Background()

	email := "Admin@Manova.Space"
	salt := "test-salt-123456"
	code := "123456"
	codeHash := owner.HashCode(code, salt)
	now := time.Now().UTC()

	ch := &owner.Challenge{
		Email:       email,
		CodeHash:    codeHash,
		Salt:        salt,
		Attempts:    0,
		MaxAttempts: 3,
		CreatedAt:   now,
		ExpiresAt:   now.Add(10 * time.Minute),
	}

	if err := store.SaveChallenge(ctx, ch); err != nil {
		t.Fatalf("SaveChallenge failed: %v", err)
	}

	if ch.ID == "" {
		t.Errorf("expected SaveChallenge to populate ID if empty")
	}

	// 1. GetActive with mixed case email
	active, err := store.GetActiveChallenge(ctx, "admin@manova.space")
	if err != nil {
		t.Fatalf("GetActiveChallenge failed: %v", err)
	}

	if active.ID != ch.ID {
		t.Errorf("expected challenge ID %s, got %s", ch.ID, active.ID)
	}
	if active.Email != "admin@manova.space" {
		t.Errorf("expected normalized email admin@manova.space, got %s", active.Email)
	}
	if active.CodeHash != codeHash {
		t.Errorf("expected codeHash %s, got %s", codeHash, active.CodeHash)
	}
	if active.Salt != salt {
		t.Errorf("expected salt %s, got %s", salt, active.Salt)
	}
	if active.Attempts != 0 {
		t.Errorf("expected attempts 0, got %d", active.Attempts)
	}
	if active.MaxAttempts != 3 {
		t.Errorf("expected max_attempts 3, got %d", active.MaxAttempts)
	}
	if active.Verified {
		t.Errorf("expected active challenge to be unverified")
	}

	// 2. Non-existent email
	_, err = store.GetActiveChallenge(ctx, "nonexistent@manova.space")
	if !errors.Is(err, owner.ErrChallengeNotFound) {
		t.Fatalf("expected ErrChallengeNotFound for non-existent email, got %v", err)
	}

	// 3. Empty email
	_, err = store.GetActiveChallenge(ctx, "")
	if !errors.Is(err, owner.ErrChallengeNotFound) {
		t.Fatalf("expected ErrChallengeNotFound for empty email, got %v", err)
	}
}

func TestChallenges_IncrementAttempts(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Challenges()
	ctx := context.Background()

	now := time.Now().UTC()
	ch := &owner.Challenge{
		Email:       "retry@manova.space",
		CodeHash:    "dummy-hash",
		Salt:        "dummy-salt",
		Attempts:    0,
		MaxAttempts: 3,
		CreatedAt:   now,
		ExpiresAt:   now.Add(5 * time.Minute),
	}

	if err := store.SaveChallenge(ctx, ch); err != nil {
		t.Fatalf("SaveChallenge failed: %v", err)
	}

	// Increment 1
	count, err := store.IncrementAttempts(ctx, ch.ID)
	if err != nil {
		t.Fatalf("IncrementAttempts (1) failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// Increment 2
	count, err = store.IncrementAttempts(ctx, ch.ID)
	if err != nil {
		t.Fatalf("IncrementAttempts (2) failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	// Verify via GetActiveChallenge
	active, err := store.GetActiveChallenge(ctx, "retry@manova.space")
	if err != nil {
		t.Fatalf("GetActiveChallenge failed: %v", err)
	}
	if active.Attempts != 2 {
		t.Errorf("expected 2 attempts recorded in DB, got %d", active.Attempts)
	}

	// Increment non-existent ID
	_, err = store.IncrementAttempts(ctx, "non-existent-id")
	if !errors.Is(err, owner.ErrChallengeNotFound) {
		t.Fatalf("expected ErrChallengeNotFound for invalid ID, got %v", err)
	}
}

func TestChallenges_MarkVerified(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Challenges()
	ctx := context.Background()

	now := time.Now().UTC()
	ch := &owner.Challenge{
		Email:       "verify@manova.space",
		CodeHash:    "dummy-hash",
		Salt:        "dummy-salt",
		CreatedAt:   now,
		ExpiresAt:   now.Add(5 * time.Minute),
		MaxAttempts: 3,
	}

	if err := store.SaveChallenge(ctx, ch); err != nil {
		t.Fatalf("SaveChallenge failed: %v", err)
	}

	if err := store.MarkVerified(ctx, ch.ID); err != nil {
		t.Fatalf("MarkVerified failed: %v", err)
	}

	// Active challenge query should now return not found because verified == 1
	_, err := store.GetActiveChallenge(ctx, "verify@manova.space")
	if !errors.Is(err, owner.ErrChallengeNotFound) {
		t.Fatalf("expected ErrChallengeNotFound after verification, got %v", err)
	}

	// MarkVerified on non-existent ID
	err = store.MarkVerified(ctx, "non-existent-id")
	if !errors.Is(err, owner.ErrChallengeNotFound) {
		t.Fatalf("expected ErrChallengeNotFound for non-existent ID, got %v", err)
	}
}

func TestChallenges_PruneExpired(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Challenges()
	ctx := context.Background()

	past := time.Now().UTC().Add(-10 * time.Minute)
	future := time.Now().UTC().Add(10 * time.Minute)

	// Expired challenge
	expiredCh := &owner.Challenge{
		ID:          "expired-ch-1",
		Email:       "expired@manova.space",
		CodeHash:    "dummy-hash",
		Salt:        "dummy-salt",
		CreatedAt:   past.Add(-5 * time.Minute),
		ExpiresAt:   past,
		MaxAttempts: 3,
	}
	if err := store.SaveChallenge(ctx, expiredCh); err != nil {
		t.Fatalf("SaveChallenge (expired) failed: %v", err)
	}

	// Active challenge
	activeCh := &owner.Challenge{
		ID:          "active-ch-1",
		Email:       "active@manova.space",
		CodeHash:    "dummy-hash",
		Salt:        "dummy-salt",
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   future,
		MaxAttempts: 3,
	}
	if err := store.SaveChallenge(ctx, activeCh); err != nil {
		t.Fatalf("SaveChallenge (active) failed: %v", err)
	}

	// Prune expired
	if err := store.PruneExpired(ctx); err != nil {
		t.Fatalf("PruneExpired failed: %v", err)
	}

	// Expired challenge should be deleted
	_, err := store.GetActiveChallenge(ctx, "expired@manova.space")
	if !errors.Is(err, owner.ErrChallengeNotFound) {
		t.Fatalf("expected ErrChallengeNotFound for pruned challenge, got %v", err)
	}

	// Active challenge should still be present
	active, err := store.GetActiveChallenge(ctx, "active@manova.space")
	if err != nil {
		t.Fatalf("GetActiveChallenge failed for active challenge: %v", err)
	}
	if active.ID != "active-ch-1" {
		t.Errorf("expected active-ch-1, got %s", active.ID)
	}
}

func TestChallenges_LatestActiveOrdering(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Challenges()
	ctx := context.Background()

	email := "ordered@manova.space"
	t1 := time.Now().UTC().Add(-2 * time.Minute)
	t2 := time.Now().UTC()

	ch1 := &owner.Challenge{
		ID:          "ch-older",
		Email:       email,
		CodeHash:    "hash-1",
		Salt:        "salt-1",
		CreatedAt:   t1,
		ExpiresAt:   t1.Add(10 * time.Minute),
		MaxAttempts: 3,
	}
	ch2 := &owner.Challenge{
		ID:          "ch-newer",
		Email:       email,
		CodeHash:    "hash-2",
		Salt:        "salt-2",
		CreatedAt:   t2,
		ExpiresAt:   t2.Add(10 * time.Minute),
		MaxAttempts: 3,
	}

	if err := store.SaveChallenge(ctx, ch1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveChallenge(ctx, ch2); err != nil {
		t.Fatal(err)
	}

	active, err := store.GetActiveChallenge(ctx, email)
	if err != nil {
		t.Fatalf("GetActiveChallenge failed: %v", err)
	}
	if active.ID != "ch-newer" {
		t.Errorf("expected newest challenge 'ch-newer', got %s", active.ID)
	}
}

func TestChallenges_Validation(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.Challenges()
	ctx := context.Background()

	if err := store.SaveChallenge(ctx, nil); err == nil {
		t.Errorf("expected error saving nil challenge, got nil")
	}

	if err := store.SaveChallenge(ctx, &owner.Challenge{Email: ""}); err == nil {
		t.Errorf("expected error saving challenge with empty email, got nil")
	}
}
