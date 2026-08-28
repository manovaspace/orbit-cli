package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingManifestReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Version != 1 {
		t.Fatalf("version: got %d want 1", m.Version)
	}
	if len(m.Objects) != 0 {
		t.Fatalf("objects: got %d want 0", len(m.Objects))
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Manifest{
		Version: 1,
		Objects: []Object{{
			Path:        "docs/a.pdf",
			SHA256:      "aabbcc",
			Size:        12,
			ContentType: "application/pdf",
		}},
	}
	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != 1 || len(got.Objects) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got.Objects[0].Path != "docs/a.pdf" || got.Objects[0].SHA256 != "aabbcc" {
		t.Fatalf("object: %+v", got.Objects[0])
	}
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err != nil {
		t.Fatalf("manifest file: %v", err)
	}
}

func TestUpsertReplacesSamePath(t *testing.T) {
	m := Manifest{Version: 1, Objects: []Object{{Path: "a.pdf", SHA256: "old", Size: 1}}}
	m.Upsert(Object{Path: "a.pdf", SHA256: "new", Size: 2})
	if len(m.Objects) != 1 {
		t.Fatalf("len=%d", len(m.Objects))
	}
	if m.Objects[0].SHA256 != "new" || m.Objects[0].Size != 2 {
		t.Fatalf("%+v", m.Objects[0])
	}
}
