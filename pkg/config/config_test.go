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
	if cfg.Admin.Name != "Alireza" {
		t.Fatalf("expected admin name Alireza, got %s", cfg.Admin.Name)
	}
	if cfg.SMTP.Host != "mail.manova.space" {
		t.Fatalf("expected smtp host mail.manova.space, got %s", cfg.SMTP.Host)
	}
	if cfg.Defaults.Scope != "core" {
		t.Fatalf("expected default scope 'core', got %s", cfg.Defaults.Scope)
	}
	if cfg.Defaults.ExpiryDays != 7 {
		t.Fatalf("expected default expiry days 7, got %d", cfg.Defaults.ExpiryDays)
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

func TestResolveFallbackEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "fallback", "config.yaml")

	// Legacy env aliases must be ignored; only ORBIT_* vars are read.
	t.Setenv("ORBIT_SERVER_URL", "https://fallback.server.local")
	t.Setenv("ORBIT_OWNER_EMAIL", "fallback.owner@local")
	t.Setenv("ORBIT_OWNER_NAME", "FallbackOwner")
	t.Setenv("SMTP_HOST", "fallback.smtp.local")
	t.Setenv("SMTP_PORT", "5870")
	t.Setenv("SMTP_USER", "fallbackuser")
	t.Setenv("SMTP_PASS", "fallbackpass")
	t.Setenv("SMTP_FROM", "fallback@local")

	resolved, err := Resolve(ResolveOptions{
		ConfigPath: cfgPath,
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	def := DefaultConfig()
	if resolved.Server.URL != def.Server.URL {
		t.Errorf("expected default server URL %s, got %s", def.Server.URL, resolved.Server.URL)
	}
	if resolved.Admin.Email != def.Admin.Email {
		t.Errorf("expected default admin email %s, got %s", def.Admin.Email, resolved.Admin.Email)
	}
	if resolved.Admin.Name != def.Admin.Name {
		t.Errorf("expected default admin name %s, got %s", def.Admin.Name, resolved.Admin.Name)
	}
	if resolved.SMTP.Host != def.SMTP.Host {
		t.Errorf("expected default smtp host %s, got %s", def.SMTP.Host, resolved.SMTP.Host)
	}
	if resolved.SMTP.Port != def.SMTP.Port {
		t.Errorf("expected default smtp port %d, got %d", def.SMTP.Port, resolved.SMTP.Port)
	}
	if resolved.SMTP.User != def.SMTP.User {
		t.Errorf("expected default smtp user %q, got %q", def.SMTP.User, resolved.SMTP.User)
	}
	if resolved.SMTP.Pass != def.SMTP.Pass {
		t.Errorf("expected default smtp pass %q, got %q", def.SMTP.Pass, resolved.SMTP.Pass)
	}
	if resolved.SMTP.From != def.SMTP.From {
		t.Errorf("expected default smtp from %s, got %s", def.SMTP.From, resolved.SMTP.From)
	}
}

func TestResolveOrbitEnv(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "orbit", "config.yaml")

	t.Setenv("ORBIT_SERVER", "https://orbit.server.local")
	t.Setenv("ORBIT_ADMIN_EMAIL", "orbit.admin@local")
	t.Setenv("ORBIT_ADMIN_NAME", "OrbitAdmin")
	t.Setenv("ORBIT_SMTP_HOST", "orbit.smtp.local")
	t.Setenv("ORBIT_SMTP_PORT", "1587")
	t.Setenv("ORBIT_SMTP_USER", "orbituser")
	t.Setenv("ORBIT_SMTP_PASS", "orbitpass")
	t.Setenv("ORBIT_SMTP_FROM", "orbit@local")

	resolved, err := Resolve(ResolveOptions{
		ConfigPath: cfgPath,
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if resolved.Server.URL != "https://orbit.server.local" {
		t.Errorf("expected ORBIT_SERVER URL, got %s", resolved.Server.URL)
	}
	if resolved.Admin.Email != "orbit.admin@local" {
		t.Errorf("expected ORBIT_ADMIN_EMAIL, got %s", resolved.Admin.Email)
	}
	if resolved.Admin.Name != "OrbitAdmin" {
		t.Errorf("expected ORBIT_ADMIN_NAME, got %s", resolved.Admin.Name)
	}
	if resolved.SMTP.Host != "orbit.smtp.local" {
		t.Errorf("expected ORBIT_SMTP_HOST, got %s", resolved.SMTP.Host)
	}
	if resolved.SMTP.Port != 1587 {
		t.Errorf("expected ORBIT_SMTP_PORT, got %d", resolved.SMTP.Port)
	}
	if resolved.SMTP.User != "orbituser" {
		t.Errorf("expected ORBIT_SMTP_USER, got %s", resolved.SMTP.User)
	}
	if resolved.SMTP.Pass != "orbitpass" {
		t.Errorf("expected ORBIT_SMTP_PASS, got %s", resolved.SMTP.Pass)
	}
	if resolved.SMTP.From != "orbit@local" {
		t.Errorf("expected ORBIT_SMTP_FROM, got %s", resolved.SMTP.From)
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
		{"defaults.scope", "extended"},
		{"defaults.expiry_days", "14"},
		{"defaults.expirydays", "30"},
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

	// Invalid expiry_days
	if err := cfg.Set("defaults.expiry_days", "not-a-number"); err == nil {
		t.Errorf("expected error for non-integer expiry_days")
	}
	if err := cfg.Set("defaults.expiry_days", "-5"); err == nil {
		t.Errorf("expected error for negative expiry_days")
	}
	if err := cfg.Set("defaults.expiry_days", "0"); err == nil {
		t.Errorf("expected error for zero expiry_days")
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

func TestLoadAutoTightensPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "insecure-config.yaml")

	cfg := DefaultConfig()
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Deliberately make file world-readable (0644)
	if err := os.Chmod(cfgPath, 0644); err != nil {
		t.Fatalf("Chmod 0644 failed: %v", err)
	}

	fi, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0644 {
		t.Fatalf("expected 0644 permissions before load, got %o", perm)
	}

	// Load should auto-tighten to 0600
	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatalf("expected loaded config, got nil")
	}

	fiAfter, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat failed after load: %v", err)
	}
	if perm := fiAfter.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions after Load(), got %o", perm)
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
