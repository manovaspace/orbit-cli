package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/session"
	"github.com/manovaspace/orbit-cli/pkg/worker"
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
	if !strings.Contains(out, "manova onboard --resume") {
		t.Errorf("expected output to suggest 'manova onboard --resume', got:\n%s", out)
	}
}

func TestRenderUpdateBanner(t *testing.T) {
	highlights := []string{
		"Auto-configure shell autocompletion for bash and zsh",
		"Link tab-completion to optional 'm' shortcut alias",
		"Clean shell RC configurations during 'uninstall'",
		"Extra feature 4",
		"Extra feature 5",
		"Extra feature 6 (should be truncated to 5)",
	}

	rendered := renderUpdateBanner("v0.1.9", "v0.2.0", highlights)

	if !strings.Contains(rendered, "New release available:") {
		t.Errorf("expected banner to contain 'New release available:', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "v0.1.9") || !strings.Contains(rendered, "v0.2.0") {
		t.Errorf("expected banner to show versions v0.1.9 and v0.2.0, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Release Highlights:") {
		t.Errorf("expected banner to contain 'Release Highlights:', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Auto-configure shell autocompletion") {
		t.Errorf("expected banner to include highlight items, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "manova self-update") {
		t.Errorf("expected banner to include update instruction, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "Extra feature 6") {
		t.Errorf("expected 6th highlight to be truncated to Top 5, got:\n%s", rendered)
	}
}

func TestPersistentPostRun_UpdateBanner(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

	// Save and restore global version
	oldVersion := version
	version = "v0.1.9"
	defer func() { version = oldVersion }()

	// Write mock edge-version.json with newer version v0.2.0
	statePath := filepath.Join(tempHome, ".manova", "edge-version.json")
	if err := worker.WriteStateAtomic(statePath, &worker.EdgeVersionState{
		LatestVersion: "v0.2.0",
		ServerStatus:  "ok",
		LastCheckedAt: time.Now().UTC(),
		Highlights: []string{
			"Auto-configure shell autocompletion for bash and zsh",
			"Link tab-completion to optional 'm' shortcut alias",
			"Clean shell RC configurations during 'uninstall'",
		},
	}); err != nil {
		t.Fatalf("WriteStateAtomic failed: %v", err)
	}

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
	if !strings.Contains(out, "v0.1.9") || !strings.Contains(out, "v0.2.0") {
		t.Errorf("expected banner to mention version update, got:\n%s", out)
	}
	if !strings.Contains(out, "Auto-configure shell autocompletion") {
		t.Errorf("expected banner to list highlights, got:\n%s", out)
	}

	// 2. Skip commands (version, self-update, uninstall, worker) should NOT show banner
	skipArgs := [][]string{
		{"version"},
		{"self-update", "--help"},
		{"uninstall", "--help"},
		{"worker", "status"},
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
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

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
	if strings.Contains(out, "manova onboard --resume") {
		t.Errorf("unexpected resume suggestion after uninstall:\n%s", out)
	}
}


