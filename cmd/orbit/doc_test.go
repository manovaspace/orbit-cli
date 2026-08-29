package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra/doc"
)

func TestGeneratedManPagesIncludeStaffRecreate(t *testing.T) {
	dir := t.TempDir()
	header := &doc.GenManHeader{
		Title:   "ORBIT",
		Section: "1",
		Source:  "Orbit Developer Platform",
		Manual:  "Orbit Platform Manual",
	}
	if err := doc.GenManTree(newRootCmd(), header, dir); err != nil {
		t.Fatalf("GenManTree: %v", err)
	}
	path := filepath.Join(dir, "orbit-staff-recreate.1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(body)
	if !strings.Contains(text, "recreate") {
		t.Fatalf("man page missing recreate: %s", text)
	}
	if !strings.Contains(text, "--totp") {
		t.Fatalf("man page missing --totp: %s", text)
	}
}

func TestGeneratedMarkdownIncludesStaffResetTOTP(t *testing.T) {
	dir := t.TempDir()
	if err := doc.GenMarkdownTree(newRootCmd(), dir); err != nil {
		t.Fatalf("GenMarkdownTree: %v", err)
	}
	path := filepath.Join(dir, "orbit_staff_reset-password.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(body), "--totp") {
		t.Fatalf("markdown missing --totp: %s", body)
	}
}
