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
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

	fakeConfig := filepath.Join(tmpHome, ".manova")
	_ = os.MkdirAll(fakeConfig, 0755)
	_ = os.WriteFile(filepath.Join(fakeConfig, "test.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(fakeConfig, "edge-version.json"), []byte(`{"latest_version":"v0.2.0"}`), 0644)
	_ = os.WriteFile(filepath.Join(fakeConfig, "worker.pid"), []byte("999999"), 0644)

	outBuf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(outBuf)
	rootCmd.SetArgs([]string{"uninstall", "--yes", "--keep-config"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("uninstall execution failed: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "Stopped background worker and cleaned systemd units") {
		t.Errorf("expected worker stop confirmation in output, got: %s", outStr)
	}

	// Since --keep-config was passed, config dir should still exist
	if _, err := os.Stat(fakeConfig); err != nil {
		t.Errorf("expected config dir to be kept when --keep-config is passed")
	}
	if _, err := os.Stat(filepath.Join(fakeConfig, "test.json")); err != nil {
		t.Errorf("expected test.json to be preserved when --keep-config is passed")
	}

	// Worker state files should be removed even with --keep-config
	if _, err := os.Stat(filepath.Join(fakeConfig, "edge-version.json")); !os.IsNotExist(err) {
		t.Errorf("expected edge-version.json to be removed")
	}
	if _, err := os.Stat(filepath.Join(fakeConfig, "worker.pid")); !os.IsNotExist(err) {
		t.Errorf("expected worker.pid to be removed")
	}
}

func TestUninstallFullPurgeCleansWorkerAndConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

	fakeConfig := filepath.Join(tmpHome, ".manova")
	_ = os.MkdirAll(fakeConfig, 0755)
	_ = os.WriteFile(filepath.Join(fakeConfig, "edge-version.json"), []byte(`{"latest_version":"v0.2.0"}`), 0644)
	_ = os.WriteFile(filepath.Join(fakeConfig, "worker.pid"), []byte("999999"), 0644)
	_ = os.WriteFile(filepath.Join(fakeConfig, "session.json"), []byte("{}"), 0644)

	systemdUserDir := filepath.Join(tmpHome, ".config", "systemd", "user")
	_ = os.MkdirAll(systemdUserDir, 0755)
	_ = os.WriteFile(filepath.Join(systemdUserDir, "manova-worker.service"), []byte("service"), 0644)
	_ = os.WriteFile(filepath.Join(systemdUserDir, "manova-worker.timer"), []byte("timer"), 0644)

	outBuf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(outBuf)
	rootCmd.SetArgs([]string{"uninstall", "--yes"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("uninstall execution failed: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "Stopped background worker and cleaned systemd units") {
		t.Errorf("expected worker stop confirmation in output, got: %s", outStr)
	}

	// Full purge should delete entire .manova directory
	if _, err := os.Stat(fakeConfig); !os.IsNotExist(err) {
		t.Errorf("expected ~/.manova to be deleted during full uninstall")
	}

	// Systemd unit files should be removed
	if _, err := os.Stat(filepath.Join(systemdUserDir, "manova-worker.service")); !os.IsNotExist(err) {
		t.Errorf("expected manova-worker.service to be removed")
	}
	if _, err := os.Stat(filepath.Join(systemdUserDir, "manova-worker.timer")); !os.IsNotExist(err) {
		t.Errorf("expected manova-worker.timer to be removed")
	}
}
