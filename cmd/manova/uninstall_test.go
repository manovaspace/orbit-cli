package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"uninstall", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("uninstall help failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Uninstall the manova CLI binary") {
		t.Errorf("expected uninstall description in help, got: %s", output)
	}
	if !strings.Contains(output, "--purge-workspace") {
		t.Errorf("expected --purge-workspace flag in help, got: %s", output)
	}
}

func TestUninstallCancellation(t *testing.T) {
	inBuf := bytes.NewBufferString("n\n")
	outBuf := new(bytes.Buffer)

	rootCmd := newRootCmd()
	rootCmd.SetIn(inBuf)
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(outBuf)
	rootCmd.SetArgs([]string{"uninstall"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error on cancel: %v", err)
	}

	if !strings.Contains(outBuf.String(), "Uninstallation cancelled") {
		t.Errorf("expected cancellation message, got: %s", outBuf.String())
	}
}

func TestUninstallWithYesAndKeepConfig(t *testing.T) {
	// Setup a temporary directory acting as fake ~/.manova
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	fakeConfig := filepath.Join(tmpHome, ".manova")
	_ = os.MkdirAll(fakeConfig, 0755)
	_ = os.WriteFile(filepath.Join(fakeConfig, "test.json"), []byte("{}"), 0644)

	outBuf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(outBuf)
	rootCmd.SetArgs([]string{"uninstall", "--yes", "--keep-config"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("uninstall execution failed: %v", err)
	}

	// Since --keep-config was passed, config should still exist
	if _, err := os.Stat(fakeConfig); err != nil {
		t.Errorf("expected config to be kept when --keep-config is passed")
	}
}
