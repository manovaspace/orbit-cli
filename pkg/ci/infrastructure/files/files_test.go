package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileAndPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(path, []byte("NPM_TOKEN=abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := New()
	data, err := fs.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "NPM_TOKEN=abc\n" {
		t.Errorf("data = %q", data)
	}
	perm, err := fs.Perm(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm != 0o600 {
		t.Errorf("perm = %#o", perm)
	}
}

func TestPermMissing(t *testing.T) {
	if _, err := New().Perm(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected an error")
	}
}
