package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/config"
)

func TestGlobalConfigFlag(t *testing.T) {
	tmpDir := t.TempDir()
	customConfigPath := filepath.Join(tmpDir, "custom-orbit-config.yaml")

	customYAML := `version: 2
server:
  url: https://custom-orbit.example.com
  timeout: 42s
staff:
  url: https://custom-staff.example.com
assets:
  bucket: custom-bucket
  auto_pull: false
defaults:
  scope: custom-scope
  expiry_days: 14
ui:
  color: true
  output: json
`
	if err := os.WriteFile(customConfigPath, []byte(customYAML), 0600); err != nil {
		t.Fatalf("failed to write custom config file: %v", err)
	}

	t.Run("RootPersistentFlagRegistration", func(t *testing.T) {
		cmd := newRootCmd()
		flag := cmd.PersistentFlags().Lookup("config")
		if flag == nil {
			t.Fatal("expected --config to be registered as a persistent flag on root command")
		}
		if flag.Usage != "Custom path to Orbit CLI configuration file" {
			t.Errorf("unexpected usage string: %q", flag.Usage)
		}
	})

	t.Run("FlagBeforeSubcommand", func(t *testing.T) {
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"--config", customConfigPath, "config", "get", "server.url", "--raw"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("failed to execute with --config before subcommand: %v", err)
		}

		out := buf.String()
		if out != "https://custom-orbit.example.com" {
			t.Errorf("expected 'https://custom-orbit.example.com', got %q", out)
		}
	})

	t.Run("FlagAfterSubcommand", func(t *testing.T) {
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"config", "get", "server.url", "--raw", "--config", customConfigPath})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("failed to execute with --config after subcommand: %v", err)
		}

		out := buf.String()
		if out != "https://custom-orbit.example.com" {
			t.Errorf("expected 'https://custom-orbit.example.com', got %q", out)
		}
	})

	t.Run("FlagInMiddleOfSubcommands", func(t *testing.T) {
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"config", "--config", customConfigPath, "get", "defaults.scope", "--raw"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("failed to execute with --config in middle: %v", err)
		}

		out := buf.String()
		if out != "custom-scope" {
			t.Errorf("expected 'custom-scope', got %q", out)
		}
	})

	t.Run("ConfigShowWithRootFlag", func(t *testing.T) {
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"--config", customConfigPath, "config", "show", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("failed to execute config show with root --config: %v", err)
		}

		var cfg config.Config
		if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
			t.Fatalf("failed to parse config JSON: %v\nOutput: %s", err, buf.String())
		}

		if cfg.Server.URL != "https://custom-orbit.example.com" {
			t.Errorf("expected server url 'https://custom-orbit.example.com', got %q", cfg.Server.URL)
		}
		if cfg.Defaults.Scope != "custom-scope" {
			t.Errorf("expected defaults scope 'custom-scope', got %q", cfg.Defaults.Scope)
		}
	})

	t.Run("ConfigListWithRootFlag", func(t *testing.T) {
		buf := new(bytes.Buffer)
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"--config", customConfigPath, "config", "list", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("failed to execute config list with root --config: %v", err)
		}

		var entries []config.ConfigEntry
		if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
			t.Fatalf("failed to parse entries JSON: %v\nOutput: %s", err, buf.String())
		}

		var foundServerURL bool
		for _, e := range entries {
			if e.Key == "server.url" {
				foundServerURL = true
				if e.Value != "https://custom-orbit.example.com" {
					t.Errorf("expected server.url to be 'https://custom-orbit.example.com', got %q", e.Value)
				}
				if !strings.Contains(e.SourceRef, "custom-orbit-config.yaml") {
					t.Errorf("expected source ref to contain config path, got %q", e.SourceRef)
				}
			}
		}
		if !foundServerURL {
			t.Errorf("server.url entry not found in list output")
		}
	})
}

func TestPostRunOnboardingNoticeSuppressedWhenAdminVerified(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")
	ownerPath := filepath.Join(tmpDir, "owner.json")

	// Set env to redirect session and owner store to temp files
	t.Setenv("ORBIT_SESSION_FILE", sessionPath)
	t.Setenv("ORBIT_OWNER_STORE", ownerPath)

	// Create an uncompleted session at stage init
	sessData := `{"id":"test-sess","current_stage":"init"}`
	if err := os.WriteFile(sessionPath, []byte(sessData), 0600); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	// Case 1: Owner vault does not exist or unverified -> notice is shown
	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"port", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("port list failed: %v", err)
	}

	if !strings.Contains(buf.String(), "Ongoing onboarding session detected") {
		t.Errorf("expected onboarding notice when owner is unverified, got: %s", buf.String())
	}

	// Case 2: Owner vault exists and is verified -> notice is suppressed
	ownerData := `{"email":"admin@example.com","root_signing_secret":"0123456789abcdef0123456789abcdef","verified_at":"2026-09-01T00:00:00Z"}`
	if err := os.WriteFile(ownerPath, []byte(ownerData), 0600); err != nil {
		t.Fatalf("failed to write owner file: %v", err)
	}

	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"port", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("port list failed: %v", err)
	}

	if strings.Contains(buf.String(), "Ongoing onboarding session detected") {
		t.Errorf("expected onboarding notice to be suppressed when admin is verified, got: %s", buf.String())
	}
}
