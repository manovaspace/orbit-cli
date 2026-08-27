package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelfUpdateHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"self-update", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("self-update --help failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "manova CLI") {
		t.Errorf("expected help description in output, got: %s", output)
	}
}

func TestSubcommandRegistration(t *testing.T) {
	rootCmd := newRootCmd()
	registered := make(map[string]bool)
	for _, c := range rootCmd.Commands() {
		registered[c.Name()] = true
	}

	expectedCmds := []string{
		"init",
		"doctor",
		"status",
		"sync",
		"env",
		"port",
		"migrate",
		"update",
		"dev",
		"self-update",
		"version",
	}

	for _, name := range expectedCmds {
		if !registered[name] {
			t.Errorf("expected subcommand %q to be registered in root command", name)
		}
	}
}
