package osfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindSkipsDependencyDirectories(t *testing.T) {
	root := t.TempDir()
	want := []string{
		filepath.Join(root, "a", ".env.schema.yaml"),
		filepath.Join(root, "z", ".env.schema.yml"),
	}
	for _, path := range append(want, filepath.Join(root, "node_modules", ".env.schema.yaml")) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("version: '1'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := New().Find(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("found[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStatReportsMissingWithoutError(t *testing.T) {
	fs := New()
	if _, exists, err := fs.Stat(filepath.Join(t.TempDir(), "absent")); err != nil || exists {
		t.Fatalf("Stat(absent) = exists=%v err=%v", exists, err)
	}
}

func TestFindMissingRootFails(t *testing.T) {
	if _, err := New().Find(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing root treated as success")
	}
}
