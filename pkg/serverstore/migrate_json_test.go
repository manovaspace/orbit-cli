package serverstore_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/serverstore"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestMigrateLegacyJSON_SliceFormat(t *testing.T) {
	db := sqlite.NewTestDB(t)
	target := db.Invites()
	ctx := context.Background()

	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "invites.json")

	now := time.Now().UTC().Truncate(time.Second)
	records := []*invite.InviteRecord{
		{
			ID:          "inv_mig_1",
			Token:       "tok_mig_1",
			Email:       "mig1@example.com",
			DisplayName: "Migration One",
			Scope:       "orbit:admin",
			IssuedAt:    now,
			ExpiresAt:   now.Add(48 * time.Hour),
			Metadata:    map[string]string{"env": "prod"},
		},
		{
			ID:        "inv_mig_2",
			Token:     "tok_mig_2",
			Email:     "mig2@example.com",
			Scope:     "orbit:user",
			IssuedAt:  now,
			ExpiresAt: now.Add(24 * time.Hour),
		},
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal records: %v", err)
	}
	if err := os.WriteFile(jsonPath, data, 0600); err != nil {
		t.Fatalf("failed to write json file: %v", err)
	}

	migrated, err := serverstore.MigrateLegacyJSON(ctx, jsonPath, target)
	if err != nil {
		t.Fatalf("MigrateLegacyJSON failed: %v", err)
	}
	if migrated != 2 {
		t.Fatalf("expected 2 migrated records, got %d", migrated)
	}

	// Verify original file is gone and .bak exists
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("expected original json file to be removed / renamed, but it exists")
	}
	bakPath := jsonPath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("expected .bak file to exist at %s: %v", bakPath, err)
	}

	// Verify records in target SQLite store
	r1, err := target.GetInvite(ctx, "inv_mig_1")
	if err != nil || r1.Email != "mig1@example.com" || r1.Metadata["env"] != "prod" {
		t.Fatalf("failed to fetch migrated record 1: %v, %+v", err, r1)
	}

	r2, err := target.GetInvite(ctx, "inv_mig_2")
	if err != nil || r2.Email != "mig2@example.com" {
		t.Fatalf("failed to fetch migrated record 2: %v, %+v", err, r2)
	}

	// Running migration again on the same path (which now doesn't exist) should return (0, nil)
	again, err := serverstore.MigrateLegacyJSON(ctx, jsonPath, target)
	if err != nil {
		t.Fatalf("idempotent migration check failed: %v", err)
	}
	if again != 0 {
		t.Fatalf("expected 0 migrated on second run, got %d", again)
	}
}

func TestMigrateLegacyJSON_MapFormat(t *testing.T) {
	db := sqlite.NewTestDB(t)
	target := db.Invites()
	ctx := context.Background()

	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "map_invites.json")

	now := time.Now().UTC().Truncate(time.Second)
	records := map[string]*invite.InviteRecord{
		"inv_map_1": {
			ID:        "inv_map_1",
			Token:     "tok_map_1",
			Email:     "map1@example.com",
			Scope:     "orbit",
			IssuedAt:  now,
			ExpiresAt: now.Add(24 * time.Hour),
		},
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal records: %v", err)
	}
	if err := os.WriteFile(jsonPath, data, 0600); err != nil {
		t.Fatalf("failed to write json file: %v", err)
	}

	migrated, err := serverstore.MigrateLegacyJSON(ctx, jsonPath, target)
	if err != nil {
		t.Fatalf("MigrateLegacyJSON map format failed: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("expected 1 migrated record, got %d", migrated)
	}

	fetched, err := target.GetInvite(ctx, "inv_map_1")
	if err != nil || fetched.Email != "map1@example.com" {
		t.Fatalf("failed to fetch record migrated from map: %v, %+v", err, fetched)
	}
}

func TestMigrateLegacyJSON_CorruptJSON(t *testing.T) {
	db := sqlite.NewTestDB(t)
	target := db.Invites()
	ctx := context.Background()

	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "corrupt.json")

	if err := os.WriteFile(jsonPath, []byte("NOT_VALID_JSON{{{"), 0600); err != nil {
		t.Fatalf("failed to write corrupt json file: %v", err)
	}

	_, err := serverstore.MigrateLegacyJSON(ctx, jsonPath, target)
	if err == nil {
		t.Fatal("expected error migrating corrupt JSON file, got nil")
	}

	// File should NOT be renamed to .bak if parsing fails
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("expected original corrupt file to still exist: %v", err)
	}
}

func TestAutoMigrateLegacyFiles(t *testing.T) {
	db := sqlite.NewTestDB(t)
	target := db.Invites()
	ctx := context.Background()

	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "env_invites.json")

	now := time.Now().UTC().Truncate(time.Second)
	records := []*invite.InviteRecord{
		{
			ID:        "inv_auto_1",
			Token:     "tok_auto_1",
			Email:     "auto@example.com",
			Scope:     "orbit",
			IssuedAt:  now,
			ExpiresAt: now.Add(24 * time.Hour),
		},
	}

	data, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("failed to marshal records: %v", err)
	}
	if err := os.WriteFile(envPath, data, 0600); err != nil {
		t.Fatalf("failed to write env invites file: %v", err)
	}

	// Set ORBIT_INVITES_FILE environment variable
	t.Setenv("ORBIT_INVITES_FILE", envPath)

	migrated, err := serverstore.AutoMigrateLegacyFiles(ctx, target)
	if err != nil {
		t.Fatalf("AutoMigrateLegacyFiles failed: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("expected 1 migrated record, got %d", migrated)
	}

	// Verify persistence in SQLite
	fetched, err := target.GetInvite(ctx, "inv_auto_1")
	if err != nil || fetched.Email != "auto@example.com" {
		t.Fatalf("failed to fetch auto-migrated record: %v, %+v", err, fetched)
	}

	// Verify idempotency
	again, err := serverstore.AutoMigrateLegacyFiles(ctx, target)
	if err != nil {
		t.Fatalf("second AutoMigrateLegacyFiles failed: %v", err)
	}
	if again != 0 {
		t.Fatalf("expected 0 migrated records on rerun, got %d", again)
	}
}
