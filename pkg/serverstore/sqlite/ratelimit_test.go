package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestRateLimitStore_RecordAndCount(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.RateLimits()
	if store == nil {
		t.Fatal("expected non-nil RateLimitStore from db.RateLimits()")
	}

	ctx := context.Background()
	now := time.Now().UTC()

	// Record events
	key1 := "192.0.2.1"
	route1 := "/api/v1/onboard/claim"

	// Event 1: 5 minutes ago
	if err := store.RecordEvent(ctx, key1, route1, now.Add(-5*time.Minute)); err != nil {
		t.Fatalf("RecordEvent failed: %v", err)
	}

	// Event 2: 1 minute ago
	if err := store.RecordEvent(ctx, key1, route1, now.Add(-1*time.Minute)); err != nil {
		t.Fatalf("RecordEvent failed: %v", err)
	}

	// Event 3: now
	if err := store.RecordEvent(ctx, key1, route1, now); err != nil {
		t.Fatalf("RecordEvent failed: %v", err)
	}

	// Count events in last 2 minutes -> should be 2 (Event 2 and Event 3)
	count2m, err := store.CountEventsSince(ctx, key1, route1, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("CountEventsSince (2m) failed: %v", err)
	}
	if count2m != 2 {
		t.Errorf("expected 2 events in last 2m, got %d", count2m)
	}

	// Count events in last 10 minutes -> should be 3
	count10m, err := store.CountEventsSince(ctx, key1, route1, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("CountEventsSince (10m) failed: %v", err)
	}
	if count10m != 3 {
		t.Errorf("expected 3 events in last 10m, got %d", count10m)
	}

	// Route isolation: count for different route -> 0
	countOtherRoute, err := store.CountEventsSince(ctx, key1, "/api/v1/admin/challenge", now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("CountEventsSince (other route) failed: %v", err)
	}
	if countOtherRoute != 0 {
		t.Errorf("expected 0 events for different route, got %d", countOtherRoute)
	}

	// Key isolation: count for different key -> 0
	countOtherKey, err := store.CountEventsSince(ctx, "198.51.100.2", route1, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("CountEventsSince (other key) failed: %v", err)
	}
	if countOtherKey != 0 {
		t.Errorf("expected 0 events for different key, got %d", countOtherKey)
	}
}

func TestRateLimitStore_PruneEventsOlderThan(t *testing.T) {
	db := sqlite.NewTestDB(t)
	store := db.RateLimits()
	ctx := context.Background()
	now := time.Now().UTC()

	key := "user@example.com"
	route := "/api/v1/admin/challenge"

	// Event 1: 1 hour ago
	_ = store.RecordEvent(ctx, key, route, now.Add(-1*time.Hour))
	// Event 2: 30 minutes ago
	_ = store.RecordEvent(ctx, key, route, now.Add(-30*time.Minute))
	// Event 3: 5 minutes ago
	_ = store.RecordEvent(ctx, key, route, now.Add(-5*time.Minute))

	// Prune older than 15 minutes ago (should delete Event 1 and 2)
	cutoff := now.Add(-15 * time.Minute)
	if err := store.PruneEventsOlderThan(ctx, cutoff); err != nil {
		t.Fatalf("PruneEventsOlderThan failed: %v", err)
	}

	// Total events remaining since 2 hours ago should now be 1
	count, err := store.CountEventsSince(ctx, key, route, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("CountEventsSince failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 event remaining after prune, got %d", count)
	}
}
