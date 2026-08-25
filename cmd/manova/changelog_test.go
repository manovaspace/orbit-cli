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
	cmd.SetArgs([]string{"changelog"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("changelog execution failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Manova Changelog & Release Notes") {
		t.Errorf("expected header in changelog output, got:\n%s", out)
	}
	if !strings.Contains(out, "v0.3.0") {
		t.Errorf("expected v0.3.0 in output, got:\n%s", out)
	}
}

func TestChangelogCmd_WhatsnewAlias(t *testing.T) {
	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"whatsnew", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("whatsnew execution failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "v0.3.0") {
		t.Errorf("expected v0.3.0 in output, got:\n%s", out)
	}
}

func TestChangelogCmd_SpecificVersion(t *testing.T) {
	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"changelog", "--version", "v0.2.9"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("changelog --version v0.2.9 failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "v0.2.9") || !strings.Contains(out, "manova user") {
		t.Errorf("expected v0.2.9 user highlights, got:\n%s", out)
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
	if !strings.Contains(out, `"version": "v0.3.0"`) && !strings.Contains(out, `"version":`) {
		t.Errorf("expected json array with version field, got:\n%s", out)
	}
}
