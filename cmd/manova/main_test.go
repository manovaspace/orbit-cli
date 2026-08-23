package main

import (
	"bytes"
	"strings"
	"testing"

	"git.dev.manova.space/manova/orbit-cli/pkg/session"
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

