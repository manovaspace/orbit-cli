package notifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ReadStore reads the MessageStore from storePath. Returns an empty store if file missing.
func ReadStore(storePath string) (*MessageStore, error) {
	if storePath == "" {
		storePath = DefaultStoreFile
	}
	data, err := os.ReadFile(expandPath(storePath))
	if os.IsNotExist(err) {
		return &MessageStore{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s MessageStore
	if err := json.Unmarshal(data, &s); err != nil {
		return &MessageStore{}, nil // corrupt file treated as empty
	}
	return &s, nil
}

// WriteStoreAtomic writes the MessageStore atomically via temp-file rename.
func WriteStoreAtomic(storePath string, store *MessageStore) error {
	if storePath == "" {
		storePath = DefaultStoreFile
	}
	dst := expandPath(storePath)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	store.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".msgstore-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Chmod(0644)
	_ = tmp.Sync()
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

// IsSeen returns true if the given message ID is in the store's Seen list.
func IsSeen(store *MessageStore, id string) bool {
	for _, s := range store.Seen {
		if s == id {
			return true
		}
	}
	return false
}

// MarkSeen adds id to the store's Seen list (idempotent) and persists atomically.
func MarkSeen(storePath, id string) error {
	store, err := ReadStore(storePath)
	if err != nil {
		return err
	}
	if IsSeen(store, id) {
		return nil
	}
	store.Seen = append(store.Seen, id)
	return WriteStoreAtomic(storePath, store)
}

// FilterVisible returns messages that should be displayed to the user.
// Filters out expired messages and non-critical already-seen messages.
// Caps results at MaxMessagesPerRun.
func FilterVisible(messages []Message, store *MessageStore) []Message {
	var visible []Message
	for _, m := range messages {
		if m.IsExpired() {
			continue
		}
		if !m.IsCritical() && IsSeen(store, m.ID) {
			continue
		}
		visible = append(visible, m)
		if len(visible) >= MaxMessagesPerRun {
			break
		}
	}
	return visible
}
