package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaffOwnerGuard_Unverified(t *testing.T) {
	tempDir := t.TempDir()
	unverifiedOwnerPath := filepath.Join(tempDir, "nonexistent-owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "list",
		"--owner-store", unverifiedOwnerPath,
		"--server", "http://127.0.0.1:9",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when owner is unverified")
	}
	if !strings.Contains(err.Error(), staffOwnerUnverified) {
		t.Fatalf("expected error %q, got %q", staffOwnerUnverified, err.Error())
	}
}

func TestStaffCmdRegistered(t *testing.T) {
	cmd := newRootCmd()
	found := false
	for _, c := range cmd.Commands() {
		if c.Name() == "staff" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("staff command not registered")
	}
}
