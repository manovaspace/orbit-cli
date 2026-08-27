package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDevPortalCmd(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"dev", "portal"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dev portal failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "10007") {
		t.Errorf("expected portal port 10007 in output, got: %s", output)
	}
	if !strings.Contains(output, "Developer Portal") {
		t.Errorf("expected header in output, got: %s", output)
	}
}

func TestDevSubcommandsTree(t *testing.T) {
	devCmd := newDevCmd()
	subcommands := make(map[string]bool)
	for _, c := range devCmd.Commands() {
		subcommands[c.Name()] = true
	}

	expected := []string{"up", "down", "tier2", "caddy", "portal", "logs"}
	for _, exp := range expected {
		if !subcommands[exp] {
			t.Errorf("expected dev subcommand %q to be registered", exp)
		}
	}
}
