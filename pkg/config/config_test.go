package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.URL != "https://orbit.manova.space" {
		t.Fatalf("expected server url https://orbit.manova.space, got %s", cfg.Server.URL)
	}
	if cfg.Admin.Email != "alirezaopmc@gmail.com" {
		t.Fatalf("expected admin email alirezaopmc@gmail.com, got %s", cfg.Admin.Email)
	}
	if cfg.SMTP.Host != "mail.manova.space" {
		t.Fatalf("expected smtp host mail.manova.space, got %s", cfg.SMTP.Host)
	}
}

func TestResolveHierarchy(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Admin.Email = "file@example.com"
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// 1. File only
	resolved, err := Resolve(ResolveOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.Admin.Email != "file@example.com" {
		t.Errorf("expected file@example.com, got %s", resolved.Admin.Email)
	}

	// 2. Env override
	os.Setenv("ORBIT_ADMIN_EMAIL", "env@example.com")
	defer os.Unsetenv("ORBIT_ADMIN_EMAIL")

	resolved, err = Resolve(ResolveOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.Admin.Email != "env@example.com" {
		t.Errorf("expected env@example.com, got %s", resolved.Admin.Email)
	}

	// 3. Flag override
	resolved, err = Resolve(ResolveOptions{
		ConfigPath: cfgPath,
		OwnerFlag:  "flag@example.com",
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.Admin.Email != "flag@example.com" {
		t.Errorf("expected flag@example.com, got %s", resolved.Admin.Email)
	}
}

func TestResolveAllOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "custom", "config.yaml")

	// Set environment variables
	os.Setenv("ORBIT_SERVER", "https://env.orbit.local")
	defer os.Unsetenv("ORBIT_SERVER")
	os.Setenv("ORBIT_ADMIN_NAME", "EnvAdmin")
	defer os.Unsetenv("ORBIT_ADMIN_NAME")
	os.Setenv("ORBIT_SMTP_HOST", "smtp.env.local")
	defer os.Unsetenv("ORBIT_SMTP_HOST")
	os.Setenv("ORBIT_SMTP_PORT", "2525")
	defer os.Unsetenv("ORBIT_SMTP_PORT")
	os.Setenv("ORBIT_SMTP_USER", "envuser")
	defer os.Unsetenv("ORBIT_SMTP_USER")
	os.Setenv("ORBIT_SMTP_PASS", "envpass")
	defer os.Unsetenv("ORBIT_SMTP_PASS")
	os.Setenv("ORBIT_SMTP_FROM", "env@local")
	defer os.Unsetenv("ORBIT_SMTP_FROM")

	resolved, err := Resolve(ResolveOptions{
		ConfigPath: cfgPath,
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.Server.URL != "https://env.orbit.local" {
		t.Errorf("expected server https://env.orbit.local, got %s", resolved.Server.URL)
	}
	if resolved.Admin.Name != "EnvAdmin" {
		t.Errorf("expected admin name EnvAdmin, got %s", resolved.Admin.Name)
	}
	if resolved.SMTP.Host != "smtp.env.local" {
		t.Errorf("expected smtp host smtp.env.local, got %s", resolved.SMTP.Host)
	}
	if resolved.SMTP.Port != 2525 {
		t.Errorf("expected smtp port 2525, got %d", resolved.SMTP.Port)
	}
	if resolved.SMTP.User != "envuser" {
		t.Errorf("expected smtp user envuser, got %s", resolved.SMTP.User)
	}
	if resolved.SMTP.Pass != "envpass" {
		t.Errorf("expected smtp pass envpass, got %s", resolved.SMTP.Pass)
	}
	if resolved.SMTP.From != "env@local" {
		t.Errorf("expected smtp from env@local, got %s", resolved.SMTP.From)
	}

	// Flag overrides override environment variables
	resolvedFlags, err := Resolve(ResolveOptions{
		ConfigPath: cfgPath,
		ServerFlag: "https://flag.orbit.local",
		OwnerFlag:  "flagowner@local",
		NameFlag:   "FlagName",
		SMTPHost:   "flag.smtp.local",
		SMTPPort:   465,
		SMTPUser:   "flaguser",
		SMTPPass:   "flagpass",
		SMTPFrom:   "flag@local",
	})
	if err != nil {
		t.Fatalf("resolve with flags failed: %v", err)
	}
	if resolvedFlags.Server.URL != "https://flag.orbit.local" {
		t.Errorf("expected flag server, got %s", resolvedFlags.Server.URL)
	}
	if resolvedFlags.Admin.Email != "flagowner@local" {
		t.Errorf("expected flag email, got %s", resolvedFlags.Admin.Email)
	}
	if resolvedFlags.Admin.Name != "FlagName" {
		t.Errorf("expected flag name, got %s", resolvedFlags.Admin.Name)
	}
	if resolvedFlags.SMTP.Host != "flag.smtp.local" {
		t.Errorf("expected flag smtp host, got %s", resolvedFlags.SMTP.Host)
	}
	if resolvedFlags.SMTP.Port != 465 {
		t.Errorf("expected flag smtp port 465, got %d", resolvedFlags.SMTP.Port)
	}
	if resolvedFlags.SMTP.User != "flaguser" {
		t.Errorf("expected flag smtp user, got %s", resolvedFlags.SMTP.User)
	}
	if resolvedFlags.SMTP.Pass != "flagpass" {
		t.Errorf("expected flag smtp pass, got %s", resolvedFlags.SMTP.Pass)
	}
	if resolvedFlags.SMTP.From != "flag@local" {
		t.Errorf("expected flag smtp from, got %s", resolvedFlags.SMTP.From)
	}
}

func TestGetSetMasked(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("admin.email", "custom@example.com"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	val, err := cfg.Get("admin.email")
	if err != nil || val != "custom@example.com" {
		t.Fatalf("Get returned %q, error: %v", val, err)
	}

	// Test all key getters and setters
	testCases := []struct {
		key   string
		value string
	}{
		{"server.url", "https://custom.orbit.io"},
		{"server", "https://custom2.orbit.io"},
		{"admin.email", "admin@domain.com"},
		{"admin", "admin2@domain.com"},
		{"owner", "owner@domain.com"},
		{"admin.name", "New Admin"},
		{"smtp.host", "smtp.custom.io"},
		{"smtp.port", "465"},
		{"smtp.user", "smtpuser"},
		{"smtp.pass", "secretpassword"},
		{"smtp.from", "noreply@domain.com"},
	}

	for _, tc := range testCases {
		if err := cfg.Set(tc.key, tc.value); err != nil {
			t.Fatalf("Set(%q, %q) failed: %v", tc.key, tc.value, err)
		}
		got, err := cfg.Get(tc.key)
		if err != nil {
			t.Fatalf("Get(%q) failed: %v", tc.key, err)
		}
		if got != tc.value {
			t.Errorf("Get(%q) = %q, expected %q", tc.key, got, tc.value)
		}
	}

	// Invalid key
	if err := cfg.Set("invalid.key", "val"); err == nil {
		t.Errorf("expected error for invalid key on Set")
	}
	if _, err := cfg.Get("invalid.key"); err == nil {
		t.Errorf("expected error for invalid key on Get")
	}

	// Invalid port
	if err := cfg.Set("smtp.port", "not-a-number"); err == nil {
		t.Errorf("expected error for non-integer port")
	}
	if err := cfg.Set("smtp.port", "-1"); err == nil {
		t.Errorf("expected error for negative port")
	}

	cfg.SMTP.Pass = "supersecret"
	masked := cfg.Masked()
	if masked.SMTP.Pass != "********" {
		t.Fatalf("expected masked password, got %s", masked.SMTP.Pass)
	}
	if cfg.SMTP.Pass != "supersecret" {
		t.Fatalf("original password was modified: %s", cfg.SMTP.Pass)
	}
}

func TestLoadSaveDefaultPath(t *testing.T) {
	path := DefaultConfigPath()
	if path == "" {
		t.Fatalf("DefaultConfigPath returned empty string")
	}

	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "sub", "test-config.yaml")

	_, err := Load(customPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}

	cfg := DefaultConfig()
	cfg.Admin.Name = "TestUser"
	if err := cfg.Save(customPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(customPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Admin.Name != "TestUser" {
		t.Fatalf("expected TestUser, got %s", loaded.Admin.Name)
	}
}
