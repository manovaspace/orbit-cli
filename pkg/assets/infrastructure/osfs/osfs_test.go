package osfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, size, err := New().HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if size != 3 {
		t.Fatalf("size=%d", size)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if sum != want {
		t.Fatalf("sha256=%s want %s", sum, want)
	}
}

func TestStatMissing(t *testing.T) {
	_, exists, err := New().Stat(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if exists {
		t.Fatal("missing path reported as existing")
	}
}

func TestWriteReadRename(t *testing.T) {
	dir := t.TempDir()
	fs := New()
	path := filepath.Join(dir, "a.txt")
	if err := fs.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(dir, "b.txt")
	if err := fs.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one" {
		t.Fatalf("data = %q", data)
	}
}
