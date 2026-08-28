package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignoreAddsPath(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureGitignore(dir, "docs/a.pdf"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "docs/a.pdf") {
		t.Fatalf("gitignore missing path: %s", data)
	}
	if err := EnsureGitignore(dir, "docs/a.pdf"); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(data2), "docs/a.pdf") != 1 {
		t.Fatalf("duplicated path: %s", data2)
	}
}
