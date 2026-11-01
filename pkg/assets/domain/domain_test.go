package domain

import "testing"

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

func TestFindMissing(t *testing.T) {
	m := &Manifest{Objects: []Object{{Path: "a.pdf"}}}
	if _, ok := m.Find("b.pdf"); ok {
		t.Fatal("found a missing object")
	}
}

func TestContentTypeFor(t *testing.T) {
	tests := map[string]string{
		"a.pdf":  "application/pdf",
		"a.PNG":  "image/png",
		"a.jpeg": "image/jpeg",
		"a.svg":  "image/svg+xml",
		"a.bin":  "application/octet-stream",
	}
	for path, want := range tests {
		if got := ContentTypeFor(path); got != want {
			t.Errorf("ContentTypeFor(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestWarnSmall(t *testing.T) {
	if !WarnSmall(1024) {
		t.Error("small file not flagged")
	}
	if WarnSmall(warnUnderBytes) {
		t.Error("large file flagged")
	}
}
