package notifier_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/notifier"
)

func TestReadWriteStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "messages.json")

	store := &notifier.MessageStore{Seen: []string{"msg-1"}}
	if err := notifier.WriteStoreAtomic(path, store); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	loaded, err := notifier.ReadStore(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if len(loaded.Seen) != 1 || loaded.Seen[0] != "msg-1" {
		t.Errorf("unexpected seen: %v", loaded.Seen)
	}
}

func TestMarkSeen_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "messages.json")

	if err := notifier.MarkSeen(path, "msg-1"); err != nil {
		t.Fatalf("first MarkSeen failed: %v", err)
	}
	if err := notifier.MarkSeen(path, "msg-1"); err != nil {
		t.Fatalf("second MarkSeen failed: %v", err)
	}
	store, _ := notifier.ReadStore(path)
	count := 0
	for _, id := range store.Seen {
		if id == "msg-1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of msg-1, got %d", count)
	}
}

func TestFilterVisible_ExpiryAndSeen(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	messages := []notifier.Message{
		{ID: "expired", Type: "tip", Priority: "low", Title: "expired", ExpiresAt: &past},
		{ID: "seen-normal", Type: "tip", Priority: "normal", Title: "seen"},
		{ID: "critical-seen", Type: "alert", Priority: "critical", Title: "critical"},
		{ID: "active", Type: "release", Priority: "high", Title: "active", ExpiresAt: &future},
	}

	store := &notifier.MessageStore{Seen: []string{"seen-normal", "critical-seen"}}
	visible := notifier.FilterVisible(messages, store)

	ids := make(map[string]bool)
	for _, m := range visible {
		ids[m.ID] = true
	}

	if ids["expired"] {
		t.Error("expired message should not be visible")
	}
	if ids["seen-normal"] {
		t.Error("seen normal message should not be visible")
	}
	if !ids["critical-seen"] {
		t.Error("critical message should always be visible even when seen")
	}
	if !ids["active"] {
		t.Error("active unseen message should be visible")
	}
}
