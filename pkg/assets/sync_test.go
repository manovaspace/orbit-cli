package assets

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAddUploadsAndWritesManifest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "docs", "a.pdf")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("pdf-bytes")
	if err := os.WriteFile(src, payload, 0644); err != nil {
		t.Fatal(err)
	}
	store := NewMemStore()
	obj, err := Add(ctx, dir, "docs/a.pdf", store)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Size != int64(len(payload)) {
		t.Fatalf("size=%d", obj.Size)
	}
	got, ok := store.objects[obj.SHA256]
	if !ok {
		t.Fatal("object not uploaded")
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := m.Find("docs/a.pdf")
	if !ok || found.SHA256 != obj.SHA256 {
		t.Fatalf("manifest: %+v", m)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !bytes.Contains(gi, []byte("docs/a.pdf")) {
		t.Fatalf("gitignore: %s", gi)
	}
}

func TestAddSkipsUploadWhenHashExists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(src, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewMemStore()
	first, err := Add(ctx, dir, "a.bin", store)
	if err != nil {
		t.Fatal(err)
	}
	store.puts = 0
	second, err := Add(ctx, dir, "a.bin", store)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 {
		t.Fatalf("%s vs %s", first.SHA256, second.SHA256)
	}
	if store.puts != 0 {
		t.Fatalf("unexpected put count %d", store.puts)
	}
}

func TestPullRestoresMissingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "n.png")
	if err := os.WriteFile(src, []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewMemStore()
	if _, err := Add(ctx, dir, "n.png", store); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if err := Pull(ctx, dir, store, PullOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "png" {
		t.Fatalf("got %q", got)
	}
}

func TestPullMismatchWithoutForceErrors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "n.png")
	if err := os.WriteFile(src, []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewMemStore()
	if _, err := Add(ctx, dir, "n.png", store); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	err := Pull(ctx, dir, store, PullOptions{})
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestStatusReportsMissing(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{Version: 1, Objects: []Object{{Path: "gone.pdf", SHA256: "abc", Size: 1}}}
	if err := Save(dir, m); err != nil {
		t.Fatal(err)
	}
	st, err := Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 1 || st[0].State != FileMissing {
		t.Fatalf("%+v", st)
	}
}
