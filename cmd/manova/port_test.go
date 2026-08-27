package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPortListCmd(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"port", "list"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("port list failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "50-Port Block Allocations") {
		t.Errorf("expected header '50-Port Block Allocations', got: %s", output)
	}
	if !strings.Contains(output, "orbit-platform") || !strings.Contains(output, "fryto") {
		t.Errorf("expected project names in output, got: %s", output)
	}
	if !strings.Contains(output, "Deterministic Slots") {
		t.Errorf("expected deterministic slots section, got: %s", output)
	}
}

func TestPortAllocateCmd(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"port", "allocate", "fryto", "preview-worker"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("port allocate failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Port Allocation Successful") {
		t.Errorf("expected success message, got: %s", output)
	}
	if !strings.Contains(output, "preview-worker") {
		t.Errorf("expected service name in output, got: %s", output)
	}
	if !strings.Contains(output, "101") { // Fryto base is 10100, dynamic slots start at 10110
		t.Errorf("expected fryto port range (101xx), got: %s", output)
	}
}

func TestPortAllocateInvalidProject(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"port", "allocate", "nonexistent-project-xyz", "worker"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent project, got nil")
	}

	if !strings.Contains(err.Error(), "unknown project") {
		t.Errorf("expected 'unknown project' error, got: %v", err)
	}
}
