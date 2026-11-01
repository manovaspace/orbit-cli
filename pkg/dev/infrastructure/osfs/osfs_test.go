package osfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInfraDirUsesEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORBIT_INFRA_DIR", dir)
	if got := New().InfraDir("/workspace"); got != dir {
		t.Errorf("InfraDir = %q, want %q", got, dir)
	}
}

func TestInfraDirFindsWorkspaceCheckout(t *testing.T) {
	t.Setenv("ORBIT_INFRA_DIR", "")
	root := t.TempDir()
	infra := filepath.Join(root, "orbit", "orbit-infra")
	if err := os.MkdirAll(infra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(infra, "compose.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := New().InfraDir(root); got != infra {
		t.Errorf("InfraDir = %q, want %q", got, infra)
	}
}

func TestInfraDirDefaultsToWorkspaceLayout(t *testing.T) {
	t.Setenv("ORBIT_INFRA_DIR", "")
	root := t.TempDir()
	want := filepath.Join(root, "orbit", "orbit-infra")
	if got := New().InfraDir(root); got != want {
		t.Errorf("InfraDir = %q, want %q", got, want)
	}
}

func TestExistsAndIsDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.sh")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	fs := New()
	if !fs.Exists(file) {
		t.Error("expected file to exist")
	}
	if fs.IsDir(file) {
		t.Error("expected file not to be a directory")
	}
	if !fs.IsDir(dir) {
		t.Error("expected directory")
	}
	if fs.Exists(filepath.Join(dir, "missing")) {
		t.Error("expected missing path to be absent")
	}
}
