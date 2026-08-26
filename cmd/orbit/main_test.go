package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/session"
	"github.com/manovaspace/orbit-cli/pkg/updater"
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
	if !strings.Contains(output, "orbit version") {
		t.Errorf("expected version output to contain 'orbit version', got: %s", output)
	}
}

func TestRootCmd(t *testing.T) {
	rootCmd := newRootCmd()
	if rootCmd.Use != "orbit" {
		t.Errorf("expected Use 'orbit', got '%s'", rootCmd.Use)
	}
}

func TestPendingOnboardResumeHint(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	sm, err := session.NewSessionManager("")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	s := sm.CreateSession("test-hint@manova.space", "Test Hint")
	s.CurrentStage = session.StageReposCloned
	if err := sm.SaveSession(s); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"port", "list"})

	_ = rootCmd.Execute()
	out := buf.String()

	if !strings.Contains(out, "Ongoing onboarding session detected") {
		t.Errorf("expected output to contain onboarding session detected hint, got:\n%s", out)
	}
	if !strings.Contains(out, "orbit onboard --resume") {
		t.Errorf("expected output to suggest 'orbit onboard --resume', got:\n%s", out)
	}
}

func TestRenderUpdateBanner(t *testing.T) {
	highlights := []string{
		"Auto-configure shell autocompletion for bash and zsh",
		"Link tab-completion to optional 'o' shortcut alias",
		"Clean shell RC configurations during 'uninstall'",
		"Extra feature 4",
		"Extra feature 5",
		"Extra feature 6 (should be truncated to 5)",
	}

	rendered := renderUpdateBanner("v0.1.0", "v0.2.0", highlights)

	if !strings.Contains(rendered, "New release available:") {
		t.Errorf("expected banner to contain 'New release available:', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "v0.1.0") || !strings.Contains(rendered, "v0.2.0") {
		t.Errorf("expected banner to show versions v0.1.0 and v0.2.0, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Release Highlights:") {
		t.Errorf("expected banner to contain 'Release Highlights:', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Auto-configure shell autocompletion") {
		t.Errorf("expected banner to include highlight items, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "orbit self-update") {
		t.Errorf("expected banner to include update instruction, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "Extra feature 6") {
		t.Errorf("expected 6th highlight to be truncated to Top 5, got:\n%s", rendered)
	}
}

func TestPersistentPostRun_UpdateBanner(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Save and restore global version
	oldVersion := version
	version = "v0.1.0"
	defer func() { version = oldVersion }()

	// Write mock edge-version.json with newer version v0.2.0
	statePath := filepath.Join(tempHome, ".orbit", "edge-version.json")
	_ = os.MkdirAll(filepath.Dir(statePath), 0755)
	cacheData := updater.EdgeVersionCache{
		LatestVersion: "v0.2.0",
		ServerStatus:  "ok",
		LastCheckedAt: time.Now().UTC(),
		Highlights: []string{
			"Auto-configure shell autocompletion for bash and zsh",
			"Link tab-completion to optional 'o' shortcut alias",
			"Clean shell RC configurations during 'uninstall'",
		},
	}
	data, _ := json.MarshalIndent(cacheData, "", "  ")
	_ = os.WriteFile(statePath, data, 0644)

	// 1. Normal command (e.g. port list) should display the banner
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"port", "list"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "New release available:") {
		t.Errorf("expected update banner in normal command output, got:\n%s", out)
	}
	if !strings.Contains(out, "v0.1.0") || !strings.Contains(out, "v0.2.0") {
		t.Errorf("expected banner to mention version update, got:\n%s", out)
	}
	if !strings.Contains(out, "Auto-configure shell autocompletion") {
		t.Errorf("expected banner to list highlights, got:\n%s", out)
	}

	// 2. Skip commands (version, self-update, uninstall) should NOT show banner
	skipArgs := [][]string{
		{"version"},
		{"self-update", "--help"},
		{"uninstall", "--help"},
		{"port", "list", "--json"},
	}

	for _, args := range skipArgs {
		buf.Reset()
		cmd := newRootCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)

		_ = cmd.Execute()
		skipOut := buf.String()
		if strings.Contains(skipOut, "New release available:") {
			t.Errorf("expected banner to be skipped for args %v, but was displayed:\n%s", args, skipOut)
		}
	}
}

func TestNoPostRunNoticesOnUninstall(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	sm, err := session.NewSessionManager("")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	s := sm.CreateSession("test-uninst@manova.space", "Test Uninstall")
	s.CurrentStage = session.StageKeypairReady
	if err := sm.SaveSession(s); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"uninstall", "--yes"})

	_ = rootCmd.Execute()
	out := buf.String()

	if strings.Contains(out, "Ongoing onboarding session detected") {
		t.Errorf("unexpected onboarding session detected notice after uninstall:\n%s", out)
	}
	if strings.Contains(out, "orbit onboard --resume") {
		t.Errorf("unexpected resume suggestion after uninstall:\n%s", out)
	}
}

func TestPureCompletionOutputWithoutBanners(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	sm, err := session.NewSessionManager("")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	s := sm.CreateSession("test-comp@manova.space", "Test Comp")
	s.CurrentStage = session.StageKeypairReady
	if err := sm.SaveSession(s); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	for _, shell := range []string{"bash", "zsh"} {
		buf := new(bytes.Buffer)
		rootCmd := newRootCmd()
		rootCmd.SetOut(buf)
		rootCmd.SetErr(buf)
		rootCmd.SetArgs([]string{"completion", shell})

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("completion %s failed: %v", shell, err)
		}

		out := buf.String()
		if strings.Contains(out, "Ongoing onboarding session detected") {
			t.Errorf("completion output for %s contains onboarding banner:\n%s", shell, out)
		}
		if strings.Contains(out, "New release available:") {
			t.Errorf("completion output for %s contains update banner:\n%s", shell, out)
		}
	}
}

func TestRootHelpAliasHint(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("help execution failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Shortcut: 'o'") && !strings.Contains(out, "Shortcut") {
		t.Errorf("expected root help to contain 'Shortcut: 'o'' hint, got:\n%s", out)
	}
}

func TestRootHelpCommandCoverage(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("help execution failed: %v", err)
	}

	out := buf.String()

	expectedCommands := []string{
		"onboard",
		"init",
		"doctor",
		"dev",
		"sync",
		"status",
		"update",
		"env",
		"port",
		"migrate",
		"changelog",
		"user",
		"invite",
		"doc",
		"self-update",
		"uninstall",
		"version",
	}

	for _, cmdName := range expectedCommands {
		if !strings.Contains(out, cmdName) {
			t.Errorf("expected command %q to be listed in root help output, but was missing:\n%s", cmdName, out)
		}
	}

	// Verify group headers
	if !strings.Contains(out, "Core Commands:") {
		t.Errorf("expected 'Core Commands:' section in help")
	}
	if !strings.Contains(out, "Workspace Commands:") {
		t.Errorf("expected 'Workspace Commands:' section in help")
	}
	if !strings.Contains(out, "System & Tooling:") {
		t.Errorf("expected 'System & Tooling:' section in help")
	}
}
