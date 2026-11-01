package osfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatMissingAndDirectory(t *testing.T) {
	dir := t.TempDir()
	fs := New()

	if _, exists, err := fs.Stat(filepath.Join(dir, "absent")); err != nil || exists {
		t.Fatalf("missing path: exists=%v err=%v", exists, err)
	}
	info, exists, err := fs.Stat(dir)
	if err != nil || !exists || !info.IsDir {
		t.Fatalf("dir: info=%+v exists=%v err=%v", info, exists, err)
	}
}

func TestCopyTreePreservesFilesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "objects", "blob"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("HEAD", filepath.Join(src, "current")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := New().CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dst, "HEAD"))
	if err != nil || string(data) != "ref: refs/heads/main\n" {
		t.Fatalf("HEAD = %q, err = %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(dst, "objects", "blob")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("blob mode = %v, err = %v", info.Mode().Perm(), err)
	}
	target, err := os.Readlink(filepath.Join(dst, "current"))
	if err != nil || target != "HEAD" {
		t.Fatalf("symlink = %q, err = %v", target, err)
	}
}
