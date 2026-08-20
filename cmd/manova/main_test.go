package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "manova version") {
		t.Errorf("expected version output to contain 'manova version', got: %s", output)
	}
}

func TestRootCmd(t *testing.T) {
	rootCmd := newRootCmd()
	if rootCmd.Use != "manova" {
		t.Errorf("expected Use 'manova', got '%s'", rootCmd.Use)
	}
}
