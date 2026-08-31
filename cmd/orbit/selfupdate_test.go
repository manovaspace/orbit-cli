package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if !strings.Contains(output, "Orbit CLI") && !strings.Contains(output, "self-update") {
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
		"admin",
		"config",
	}

	for _, name := range expectedCmds {
		if !registered[name] {
			t.Errorf("expected subcommand %q to be registered in root command", name)
		}
	}

	if registered["serve"] {
		t.Errorf("expected subcommand 'serve' NOT to be registered in root command")
	}
}

func TestSelfUpdateCheckUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": version,
			"body":     "Orbit release notes",
		})
	}))
	defer srv.Close()

	t.Setenv("ORBIT_RELEASE_API_URL", srv.URL)

	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"self-update", "--check"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("self-update --check failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Orbit CLI Self-Update") {
		t.Errorf("expected title in output, got: %s", output)
	}
	if !strings.Contains(output, "already up to date") {
		t.Errorf("expected 'already up to date' message, got: %s", output)
	}
}
