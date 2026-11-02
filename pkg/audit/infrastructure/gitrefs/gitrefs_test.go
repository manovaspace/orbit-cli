package gitrefs

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHeadReadsLocalAndOriginRefs(t *testing.T) {
	dir := t.TempDir()
	git := filepath.Join(dir, ".git")
	write(t, filepath.Join(git, "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(git, "refs", "heads", "main"), "1111111111111111111111111111111111111111\n")
	write(t, filepath.Join(git, "refs", "remotes", "origin", "main"), "2222222222222222222222222222222222222222\n")

	head, origin, err := New().Head(dir, "main")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != "1111111111111111111111111111111111111111" {
		t.Errorf("head = %q", head)
	}
	if origin != "2222222222222222222222222222222222222222" {
		t.Errorf("origin = %q", origin)
	}
}

func TestHeadReadsPackedRefsAndDetachedHead(t *testing.T) {
	dir := t.TempDir()
	git := filepath.Join(dir, ".git")
	write(t, filepath.Join(git, "HEAD"), "3333333333333333333333333333333333333333\n")
	write(t, filepath.Join(git, "packed-refs"), "# pack-refs with: peeled fully-peeled sorted\n4444444444444444444444444444444444444444 refs/remotes/origin/main\n")

	head, origin, err := New().Head(dir, "main")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != "3333333333333333333333333333333333333333" || origin != "4444444444444444444444444444444444444444" {
		t.Errorf("head/origin = %q/%q", head, origin)
	}
}

func TestHeadWithoutFetchHasNoOrigin(t *testing.T) {
	dir := t.TempDir()
	git := filepath.Join(dir, ".git")
	write(t, filepath.Join(git, "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(git, "refs", "heads", "main"), "5555555555555555555555555555555555555555\n")

	head, origin, err := New().Head(dir, "main")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head == "" || origin != "" {
		t.Errorf("head/origin = %q/%q, want a head and no origin", head, origin)
	}
}

func TestHeadRejectsGitlessDirectory(t *testing.T) {
	if _, _, err := New().Head(t.TempDir(), "main"); err == nil {
		t.Fatal("gitless directory accepted")
	}
}
