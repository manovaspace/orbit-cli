package serverstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/invite"
)

// DefaultLegacyInvitePaths returns the ordered list of standard legacy invites.json locations.
func DefaultLegacyInvitePaths() []string {
	var paths []string
	if envPath := os.Getenv("ORBIT_INVITES_FILE"); envPath != "" {
		paths = append(paths, envPath)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "manova", "invites.json"))
		paths = append(paths, filepath.Join(home, ".config", "orbit", "invites.json"))
	}
	paths = append(paths, "/etc/orbit/invites.json")
	return paths
}

// MigrateLegacyJSON reads a legacy JSON file containing invite records, saves each record
// to the target InviteStore, and renames the file to <legacyPath>.bak upon success.
// If the file does not exist, it returns (0, nil).
func MigrateLegacyJSON(ctx context.Context, legacyPath string, target InviteStore) (int, error) {
	if target == nil {
		return 0, errors.New("target InviteStore cannot be nil")
	}
	if legacyPath == "" {
		return 0, nil
	}

	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("failed to stat legacy JSON file %s: %w", legacyPath, err)
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read legacy JSON file %s: %w", legacyPath, err)
	}

	records, err := parseLegacyInvites(data)
	if err != nil {
		return 0, fmt.Errorf("failed to parse legacy JSON file %s: %w", legacyPath, err)
	}

	migrated := 0
	for _, rec := range records {
		if rec == nil {
			continue
		}
		if err := target.SaveInvite(ctx, rec); err != nil {
			return migrated, fmt.Errorf("failed to save migrated invite %s: %w", rec.ID, err)
		}
		migrated++
	}

	bakPath := legacyPath + ".bak"
	if err := os.Rename(legacyPath, bakPath); err != nil {
		return migrated, fmt.Errorf("failed to rename %s to %s: %w", legacyPath, bakPath, err)
	}

	return migrated, nil
}

// AutoMigrateLegacyFiles inspects default candidate paths and migrates any present files to the target InviteStore.
func AutoMigrateLegacyFiles(ctx context.Context, target InviteStore) (int, error) {
	if target == nil {
		return 0, errors.New("target InviteStore cannot be nil")
	}

	paths := DefaultLegacyInvitePaths()
	seen := make(map[string]bool)
	total := 0

	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true

		if _, err := os.Stat(p); err == nil {
			n, err := MigrateLegacyJSON(ctx, p, target)
			if err != nil {
				return total, fmt.Errorf("auto-migration failed for %s: %w", p, err)
			}
			total += n
		}
	}

	return total, nil
}

func parseLegacyInvites(data []byte) ([]*invite.InviteRecord, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}

	// 1. Try slice format ([]*invite.InviteRecord)
	if bytes.HasPrefix(trimmed, []byte("[")) {
		var sliceRecords []*invite.InviteRecord
		if err := json.Unmarshal(trimmed, &sliceRecords); err == nil {
			return sliceRecords, nil
		}
	}

	// 2. Try map format (map[string]*invite.InviteRecord)
	if bytes.HasPrefix(trimmed, []byte("{")) {
		var mapRecords map[string]*invite.InviteRecord
		if err := json.Unmarshal(trimmed, &mapRecords); err == nil {
			records := make([]*invite.InviteRecord, 0, len(mapRecords))
			for _, rec := range mapRecords {
				if rec != nil {
					records = append(records, rec)
				}
			}
			return records, nil
		}
	}

	// 3. Fallback unmarshal attempts
	var sliceRecords []*invite.InviteRecord
	if err := json.Unmarshal(trimmed, &sliceRecords); err == nil {
		return sliceRecords, nil
	}

	var mapRecords map[string]*invite.InviteRecord
	if err := json.Unmarshal(trimmed, &mapRecords); err == nil {
		records := make([]*invite.InviteRecord, 0, len(mapRecords))
		for _, rec := range mapRecords {
			if rec != nil {
				records = append(records, rec)
			}
		}
		return records, nil
	}

	return nil, fmt.Errorf("unrecognized legacy invites JSON format")
}
