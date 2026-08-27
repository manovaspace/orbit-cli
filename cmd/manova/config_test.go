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

func TestConfigCommands(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// 1. config init
	buf := new(bytes.Buffer)
	cmd := newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"init", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config init failed: %v", err)
	}
	if !strings.Contains(buf.String(), "initialized") {
		t.Errorf("expected initialized message, got: %s", buf.String())
	}

	// Verify file was created with 0600 permissions
	fi, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat failed on config file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}

	// 1b. config init on existing file without --force
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"init", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config init on existing file failed: %v", err)
	}
	if !strings.Contains(buf.String(), "already exists") {
		t.Errorf("expected already exists message, got: %s", buf.String())
	}

	// 2. config set
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"set", "admin.email", "custom@manova.space", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	// 3. config get
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get", "admin.email", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config get failed: %v", err)
	}
	if !strings.Contains(buf.String(), "custom@manova.space") {
		t.Fatalf("expected custom@manova.space, got: %s", buf.String())
	}

	// 4. config set password & config show (masked)
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"set", "smtp.pass", "secret-password-123", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set smtp.pass failed: %v", err)
	}

	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"show", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config show failed: %v", err)
	}
	showOut := buf.String()
	if strings.Contains(showOut, "secret-password-123") {
		t.Fatalf("security violation: unmasked password in show output: %s", showOut)
	}
	if !strings.Contains(showOut, "********") {
		t.Fatalf("expected masked password in show output, got: %s", showOut)
	}
	if !strings.Contains(showOut, "custom@manova.space") {
		t.Fatalf("expected custom@manova.space in show output, got: %s", showOut)
	}

	// 5. config show --format json
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"show", "--config", cfgPath, "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config show --format json failed: %v", err)
	}
	var jsonCfg config.Config
	if err := json.Unmarshal(buf.Bytes(), &jsonCfg); err != nil {
		t.Fatalf("failed to parse JSON from config show: %v\nOutput: %s", err, buf.String())
	}
	if jsonCfg.Admin.Email != "custom@manova.space" {
		t.Errorf("expected custom@manova.space in JSON, got %s", jsonCfg.Admin.Email)
	}
	if jsonCfg.SMTP.Pass != "********" {
		t.Errorf("expected masked password in JSON, got %s", jsonCfg.SMTP.Pass)
	}

	// 6. config path
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"path", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config path failed: %v", err)
	}
	if !strings.Contains(buf.String(), cfgPath) {
		t.Fatalf("expected config path %s, got: %s", cfgPath, buf.String())
	}

	// 7. config set & get defaults.scope and defaults.expiry_days
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"set", "defaults.scope", "extended", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set defaults.scope failed: %v", err)
	}

	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get", "defaults.scope", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config get defaults.scope failed: %v", err)
	}
	if !strings.Contains(buf.String(), "extended") {
		t.Fatalf("expected extended, got: %s", buf.String())
	}

	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"set", "defaults.expiry_days", "14", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config set defaults.expiry_days failed: %v", err)
	}

	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get", "defaults.expiry_days", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config get defaults.expiry_days failed: %v", err)
	}
	if !strings.Contains(buf.String(), "14") {
		t.Fatalf("expected 14, got: %s", buf.String())
	}

	// 8. config set invalid key
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"set", "invalid.key", "value", "--config", cfgPath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error setting invalid key, got nil")
	}

	// 9. config get invalid key
	buf.Reset()
	cmd = newConfigCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get", "invalid.key", "--config", cfgPath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error getting invalid key, got nil")
	}
}

func TestConfigViaRootCmd(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"config", "init", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("manova config init failed: %v", err)
	}

	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"config", "path", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("manova config path failed: %v", err)
	}
	if !strings.Contains(buf.String(), cfgPath) {
		t.Errorf("expected config path %s, got %s", cfgPath, buf.String())
	}
}
