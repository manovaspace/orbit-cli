package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(p, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, size, err := HashFile(p)
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
