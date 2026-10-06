package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra/doc"
)

func TestDocManDefaultDateMatchesCheckedInPublicationDate(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	t.Setenv("ORBIT_SKIP_HOSTGATE", "1")
	dir := t.TempDir()
	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"doc", "-f", "man", "-o", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "orbit-version.1"))
	if err != nil {
		t.Fatal(err)
	}
	checkedIn, err := os.ReadFile(filepath.Join("..", "..", "docs", "cli", "man", "orbit-version.1"))
	if err != nil {
		t.Fatal(err)
	}
	generatedHeader := strings.Split(string(generated), "\n")[1]
	checkedInHeader := strings.Split(string(checkedIn), "\n")[1]
	if generatedHeader != checkedInHeader {
		t.Fatalf("man header changes with wall clock: got %q, want %q", generatedHeader, checkedInHeader)
	}
}

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
