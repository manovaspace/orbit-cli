package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanManPages(t *testing.T) {
	tmpDir := t.TempDir()
	man1Dir := filepath.Join(tmpDir, ".local", "share", "man", "man1")
	if err := os.MkdirAll(man1Dir, 0755); err != nil {
		t.Fatalf("failed to create mock man directory: %v", err)
	}

	// Create mock orbit man pages and unrelated man page
	orbit1 := filepath.Join(man1Dir, "orbit.1")
	orbitStaff1 := filepath.Join(man1Dir, "orbit-staff.1")
	other1 := filepath.Join(man1Dir, "other.1")

	_ = os.WriteFile(orbit1, []byte(".TH ORBIT 1"), 0644)
	_ = os.WriteFile(orbitStaff1, []byte(".TH ORBIT-STAFF 1"), 0644)
	_ = os.WriteFile(other1, []byte(".TH OTHER 1"), 0644)

	var buf bytes.Buffer
	removed := cleanManPagesWithHome(tmpDir, &buf)

	if removed != 2 {
		t.Errorf("expected 2 man pages removed, got %d", removed)
	}

	if _, err := os.Stat(orbit1); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed", orbit1)
	}
	if _, err := os.Stat(orbitStaff1); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed", orbitStaff1)
	}
	if _, err := os.Stat(other1); os.IsNotExist(err) {
		t.Errorf("expected %s to remain intact", other1)
	}
}
