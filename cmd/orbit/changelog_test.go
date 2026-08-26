package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestChangelogCmd_DefaultList(t *testing.T) {
	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"changelog", "--no-pager"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("changelog execution failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Orbit Changelog & Release Notes") {
		t.Errorf("expected header in changelog output, got:\n%s", out)
	}
	if !strings.Contains(out, "v0.1.0") {
		t.Errorf("expected v0.1.0 in output, got:\n%s", out)
	}
}

func TestChangelogCmd_SpecificVersion(t *testing.T) {
	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"changelog", "--version", "v0.1.0", "--no-pager"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("changelog --version v0.1.0 failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "v0.1.0") || !strings.Contains(out, "Orbit Platform") {
		t.Errorf("expected v0.1.0 user highlights, got:\n%s", out)
	}
}

func TestChangelogCmd_JsonOutput(t *testing.T) {
	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"changelog", "--json", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("changelog --json failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"version": "v0.1.0"`) && !strings.Contains(out, `"version":`) {
		t.Errorf("expected json array with version field, got:\n%s", out)
	}
}

func TestChangelogCmd_BoxCardRendering(t *testing.T) {
	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"changelog", "--no-pager", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("changelog failed: %v", err)
	}

	out := buf.String()
	// Verify rounded box characters are present (lipgloss RoundedBorder uses ╭ and ╰)
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╰") {
		t.Errorf("expected rounded box border characters in output, got:\n%s", out)
	}
}
