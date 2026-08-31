package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestTypesAndConstants(t *testing.T) {
	if SourceDefault != "default" {
		t.Errorf("expected SourceDefault to be 'default', got %s", SourceDefault)
	}
	if SourceUserFile != "user-config" {
		t.Errorf("expected SourceUserFile to be 'user-config', got %s", SourceUserFile)
	}
	if SourceEnv != "env" {
		t.Errorf("expected SourceEnv to be 'env', got %s", SourceEnv)
	}
	if SourceFlag != "flag" {
		t.Errorf("expected SourceFlag to be 'flag', got %s", SourceFlag)
	}

	entry := ConfigEntry{
		Key:       "server.url",
		Value:     "https://orbit.manova.space",
		Type:      "string",
		Source:    SourceDefault,
		SourceRef: "default",
	}
	if entry.Key != "server.url" || entry.Source != SourceDefault {
		t.Errorf("unexpected ConfigEntry: %+v", entry)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatalf("expected non-nil default config")
	}
	if cfg.Version != 2 {
		t.Fatalf("expected version 2, got %d", cfg.Version)
	}
	if cfg.Server.URL != "https://orbit.manova.space" {
		t.Errorf("expected default server URL https://orbit.manova.space, got %s", cfg.Server.URL)
	}
	if cfg.Server.Timeout != 15*time.Second {
		t.Errorf("expected default server timeout 15s, got %v", cfg.Server.Timeout)
	}
	if cfg.Staff.URL != "https://staff.dev.manova.space" {
		t.Errorf("expected default staff URL https://staff.dev.manova.space, got %s", cfg.Staff.URL)
	}
	if cfg.Assets.Bucket != "orbit-assets" {
		t.Errorf("expected default assets bucket 'orbit-assets', got %s", cfg.Assets.Bucket)
	}
	if cfg.Assets.Endpoint != "" {
		t.Errorf("expected default assets endpoint '', got %s", cfg.Assets.Endpoint)
	}
	if !cfg.Assets.AutoPull {
		t.Errorf("expected default assets auto_pull true, got false")
	}
	if cfg.Defaults.Scope != "all" {
		t.Errorf("expected default scope 'all', got %s", cfg.Defaults.Scope)
	}
	if cfg.Defaults.ExpiryDays != 7 {
		t.Errorf("expected default expiry days 7, got %d", cfg.Defaults.ExpiryDays)
	}
	if !cfg.UI.Color {
		t.Errorf("expected default UI color true, got false")
	}
	if cfg.UI.Output != "table" {
		t.Errorf("expected default UI output 'table', got %s", cfg.UI.Output)
	}
	if cfg.Custom == nil {
		t.Errorf("expected initialized custom map, got nil")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	// 1. Explicit ORBIT_CONFIG
	t.Run("ORBIT_CONFIG override", func(t *testing.T) {
		t.Setenv("ORBIT_CONFIG", "/custom/path/config.yaml")
		if p := DefaultConfigPath(); p != "/custom/path/config.yaml" {
			t.Errorf("expected /custom/path/config.yaml, got %s", p)
		}
	})

	// 2. XDG_CONFIG_HOME
	t.Run("XDG_CONFIG_HOME fallback", func(t *testing.T) {
		t.Setenv("ORBIT_CONFIG", "")
		t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
		expected := filepath.Join("/custom/xdg", "orbit", "config.yaml")
		if p := DefaultConfigPath(); p != expected {
			t.Errorf("expected %s, got %s", expected, p)
		}
	})

	// 3. Default ~/.config/orbit/config.yaml
	t.Run("Home directory default", func(t *testing.T) {
		t.Setenv("ORBIT_CONFIG", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get user home dir: %v", err)
		}
		expected := filepath.Join(home, ".config", "orbit", "config.yaml")
		if p := DefaultConfigPath(); p != expected {
			t.Errorf("expected %s, got %s", expected, p)
		}
	})
}

func TestGetSetMasked(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Set("server.url", "https://custom.orbit.io"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	entry, err := cfg.Get("server.url")
	if err != nil || entry.Value != "https://custom.orbit.io" {
		t.Fatalf("Get returned %q, error: %v", entry.Value, err)
	}

	testCases := []struct {
		key   string
		value string
	}{
		{"server.url", "https://custom2.orbit.io"},
		{"server", "https://custom3.orbit.io"},
		{"staff.url", "https://custom.staff.dev"},
		{"staff", "https://custom2.staff.dev"},
		{"assets.bucket", "my-bucket"},
		{"assets.endpoint", "https://r2.custom.com"},
		{"assets.auto_pull", "false"},
		{"defaults.scope", "extended"},
		{"defaults.expiry_days", "14"},
		{"defaults.expirydays", "30"},
		{"ui.color", "false"},
		{"ui.output", "json"},
	}

	for _, tc := range testCases {
		if err := cfg.Set(tc.key, tc.value); err != nil {
			t.Fatalf("Set(%q, %q) failed: %v", tc.key, tc.value, err)
		}
		got, err := cfg.Get(tc.key)
		if err != nil {
			t.Fatalf("Get(%q) failed: %v", tc.key, err)
		}
		if got.Value != tc.value {
			t.Errorf("Get(%q) = %q, expected %q", tc.key, got.Value, tc.value)
		}
	}

	// Invalid key
	if err := cfg.Set("invalid.key", "val"); err == nil {
		t.Errorf("expected error for invalid key on Set")
	}
	if _, err := cfg.Get("invalid.key"); err == nil {
		t.Errorf("expected error for invalid key on Get")
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

	// Masked returns safe copy
	masked := cfg.Masked()
	if masked == nil {
		t.Fatalf("expected non-nil masked config")
	}
	if masked.Server.URL != cfg.Server.URL {
		t.Errorf("expected server URL preserved in masked, got %s", masked.Server.URL)
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
	cfg.Server.URL = "https://custom.server.space"
	if err := cfg.Save(customPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify permissions on saved file are 0600
	fi, err := os.Stat(customPath)
	if err != nil {
		t.Fatalf("Stat on saved file failed: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions on saved file, got %o", perm)
	}

	loaded, err := Load(customPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Server.URL != "https://custom.server.space" {
		t.Fatalf("expected https://custom.server.space, got %s", loaded.Server.URL)
	}
	if loaded.Version != 2 {
		t.Fatalf("expected loaded version 2, got %d", loaded.Version)
	}
}

func TestCommentPreservation(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "commented-config.yaml")

	originalYAML := `# Orbit CLI Master Config
# Maintained by DevOps

version: 2

# Edge server connection settings
server:
  # Base API URL
  url: https://orbit.manova.space # inline server url comment
  # Request timeout duration
  timeout: 15s # inline timeout comment

# Staff control-plane connection
staff:
  url: https://staff.dev.manova.space

# Cloudflare R2 Media storage
assets:
  bucket: orbit-assets
  endpoint: ""
  auto_pull: true

# Workspace defaults
defaults:
  scope: all
  expiry_days: 7

# Terminal UI configuration
ui:
  color: true
  output: table # inline table comment

# Dynamic extensions
custom:
  # Dev mode flag
  dev_mode: false # inline dev flag comment
`

	if err := os.WriteFile(cfgPath, []byte(originalYAML), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// 1. Mutate an existing key
	if err := SetInFile(cfgPath, "server.url", "https://updated.orbit.manova.space"); err != nil {
		t.Fatalf("SetInFile(server.url) failed: %v", err)
	}

	// 2. Mutate a custom boolean key
	if err := SetInFile(cfgPath, "custom.dev_mode", "true"); err != nil {
		t.Fatalf("SetInFile(custom.dev_mode) failed: %v", err)
	}

	// 3. Add a new key under custom
	if err := SetInFile(cfgPath, "custom.new_flag", "enabled"); err != nil {
		t.Fatalf("SetInFile(custom.new_flag) failed: %v", err)
	}

	contentBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	content := string(contentBytes)

	// Verify all comments are preserved
	expectedComments := []string{
		"# Orbit CLI Master Config",
		"# Maintained by DevOps",
		"# Edge server connection settings",
		"# Base API URL",
		"# inline server url comment",
		"# Request timeout duration",
		"# inline timeout comment",
		"# Staff control-plane connection",
		"# Cloudflare R2 Media storage",
		"# Workspace defaults",
		"# Terminal UI configuration",
		"# inline table comment",
		"# Dynamic extensions",
		"# Dev mode flag",
		"# inline dev flag comment",
	}

	for _, comment := range expectedComments {
		if !strings.Contains(content, comment) {
			t.Errorf("expected comment %q to be preserved, but was missing from:\n%s", comment, content)
		}
	}

	// Verify updated values
	if !strings.Contains(content, "https://updated.orbit.manova.space") {
		t.Errorf("expected updated server.url in content:\n%s", content)
	}
	if !strings.Contains(content, "dev_mode: true") {
		t.Errorf("expected updated dev_mode in content:\n%s", content)
	}
	if !strings.Contains(content, "new_flag: enabled") {
		t.Errorf("expected new_flag in content:\n%s", content)
	}
}

func TestSetInFileUnsetInFileAST(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "test-ast-config.yaml")

	// 1. Initializing non-existent file via SetInFile
	if err := SetInFile(cfgPath, "server.url", "https://custom-init.orbit.io"); err != nil {
		t.Fatalf("SetInFile on non-existent file failed: %v", err)
	}

	// Verify file was created and can be loaded
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load on initialized config failed: %v", err)
	}
	if cfg.Server.URL != "https://custom-init.orbit.io" {
		t.Errorf("expected https://custom-init.orbit.io, got %s", cfg.Server.URL)
	}
	if cfg.Version != 2 {
		t.Errorf("expected default version 2, got %d", cfg.Version)
	}

	// 2. Set typed core values: boolean, integer, duration
	if err := SetInFile(cfgPath, "ui.color", "false"); err != nil {
		t.Fatalf("SetInFile(ui.color) failed: %v", err)
	}
	if err := SetInFile(cfgPath, "defaults.expiry_days", "14"); err != nil {
		t.Fatalf("SetInFile(defaults.expiry_days) failed: %v", err)
	}
	if err := SetInFile(cfgPath, "server.timeout", "45s"); err != nil {
		t.Fatalf("SetInFile(server.timeout) failed: %v", err)
	}

	cfg, err = Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after setting core values failed: %v", err)
	}
	if cfg.UI.Color != false {
		t.Errorf("expected UI.Color false, got %v", cfg.UI.Color)
	}
	if cfg.Defaults.ExpiryDays != 14 {
		t.Errorf("expected Defaults.ExpiryDays 14, got %d", cfg.Defaults.ExpiryDays)
	}
	if cfg.Server.Timeout != 45*time.Second {
		t.Errorf("expected Server.Timeout 45s, got %v", cfg.Server.Timeout)
	}

	// 3. Multi-level nested keys in custom.*
	if err := SetInFile(cfgPath, "custom.deep.nested.feature_flag", "true"); err != nil {
		t.Fatalf("SetInFile(custom.deep.nested.feature_flag) failed: %v", err)
	}
	if err := SetInFile(cfgPath, "custom.deep.nested.port", "10050"); err != nil {
		t.Fatalf("SetInFile(custom.deep.nested.port) failed: %v", err)
	}
	if err := SetInFile(cfgPath, "custom.deep.nested.endpoint", "https://api.internal"); err != nil {
		t.Fatalf("SetInFile(custom.deep.nested.endpoint) failed: %v", err)
	}

	cfg, err = Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after setting custom nested keys failed: %v", err)
	}
	deepMap, ok := cfg.Custom["deep"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected custom.deep to be map[string]interface{}, got %T: %+v", cfg.Custom["deep"], cfg.Custom)
	}
	nestedMap, ok := deepMap["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected custom.deep.nested to be map[string]interface{}, got %T: %+v", deepMap["nested"], deepMap)
	}
	if nestedMap["feature_flag"] != true {
		t.Errorf("expected feature_flag true, got %v (%T)", nestedMap["feature_flag"], nestedMap["feature_flag"])
	}
	if nestedMap["port"] != 10050 {
		t.Errorf("expected port 10050, got %v (%T)", nestedMap["port"], nestedMap["port"])
	}
	if nestedMap["endpoint"] != "https://api.internal" {
		t.Errorf("expected endpoint 'https://api.internal', got %v", nestedMap["endpoint"])
	}

	// 4. UnsetInFile on deep key
	if err := UnsetInFile(cfgPath, "custom.deep.nested.feature_flag"); err != nil {
		t.Fatalf("UnsetInFile(feature_flag) failed: %v", err)
	}

	cfg, err = Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after UnsetInFile failed: %v", err)
	}
	deepMap = cfg.Custom["deep"].(map[string]interface{})
	nestedMap = deepMap["nested"].(map[string]interface{})
	if _, exists := nestedMap["feature_flag"]; exists {
		t.Errorf("expected feature_flag to be removed, but was present")
	}
	if nestedMap["port"] != 10050 {
		t.Errorf("expected port 10050 to remain, got %v", nestedMap["port"])
	}

	// 5. UnsetInFile on non-existent key (idempotent no-op)
	if err := UnsetInFile(cfgPath, "non.existent.key"); err != nil {
		t.Errorf("expected UnsetInFile on non-existent key to succeed without error, got: %v", err)
	}

	// 6. UnsetInFile on non-existent file (idempotent)
	nonExistentFile := filepath.Join(tmpDir, "does-not-exist.yaml")
	if err := UnsetInFile(nonExistentFile, "some.key"); err != nil {
		t.Errorf("expected UnsetInFile on non-existent file to succeed or return nil, got: %v", err)
	}
}

func TestAtomicWritePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "nested-dir", "perm-config.yaml")

	// SetInFile creates directory and file with 0600
	if err := SetInFile(targetFile, "server.url", "https://orbit.manova.space"); err != nil {
		t.Fatalf("SetInFile failed: %v", err)
	}

	fi, err := os.Stat(targetFile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions after SetInFile, got %o", perm)
	}

	// Mutate again with SetInFile and verify 0600 is retained
	if err := SetInFile(targetFile, "ui.color", "false"); err != nil {
		t.Fatalf("SetInFile(ui.color) failed: %v", err)
	}

	fi, err = os.Stat(targetFile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions after second SetInFile, got %o", perm)
	}

	// Unset key and verify 0600 is retained
	if err := UnsetInFile(targetFile, "ui.color"); err != nil {
		t.Fatalf("UnsetInFile failed: %v", err)
	}

	fi, err = os.Stat(targetFile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions after UnsetInFile, got %o", perm)
	}
}

func TestModifyNodeAndDeleteNodeUnit(t *testing.T) {
	// Direct unit testing on AST yaml.Node
	var doc yaml.Node
	rawYAML := `version: 2
server:
  url: https://orbit.manova.space
`
	if err := yaml.Unmarshal([]byte(rawYAML), &doc); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	// 1. Error on empty parts or nil node
	if err := modifyNode(nil, []string{"server", "url"}, "test"); err == nil {
		t.Errorf("expected error for nil node in modifyNode")
	}
	if err := modifyNode(&doc, []string{}, "test"); err == nil {
		t.Errorf("expected error for empty parts in modifyNode")
	}
	if err := deleteNode(nil, []string{"server", "url"}); err == nil {
		t.Errorf("expected error for nil node in deleteNode")
	}
	if err := deleteNode(&doc, []string{}); err == nil {
		t.Errorf("expected error for empty parts in deleteNode")
	}

	// 2. modifyNode updating existing node
	if err := modifyNode(&doc, []string{"server", "url"}, "https://new.orbit.manova.space"); err != nil {
		t.Fatalf("modifyNode failed: %v", err)
	}

	// 3. modifyNode creating deep path
	if err := modifyNode(&doc, []string{"a", "b", "c"}, "123"); err != nil {
		t.Fatalf("modifyNode deep path failed: %v", err)
	}

	// 4. deleteNode deleting leaf node
	if err := deleteNode(&doc, []string{"a", "b", "c"}); err != nil {
		t.Fatalf("deleteNode failed: %v", err)
	}

	// 5. deleteNode on non-existent path should not error
	if err := deleteNode(&doc, []string{"non", "existent", "path"}); err != nil {
		t.Fatalf("deleteNode on non-existent path returned error: %v", err)
	}
}

func TestDotNotationGetSetUnset(t *testing.T) {
	cfg := DefaultConfig()

	// 1. Initial default Get
	entry, err := cfg.Get("server.url")
	if err != nil || entry.Value != DefaultServerURL || entry.Type != "string" || entry.Key != "server.url" {
		t.Fatalf("unexpected Get(server.url): %+v, err: %v", entry, err)
	}
	if entry.Source != SourceUserFile {
		t.Errorf("expected SourceUserFile, got %s", entry.Source)
	}

	entry, err = cfg.Get("server.timeout")
	if err != nil || entry.Value != "15s" || entry.Type != "duration" {
		t.Fatalf("unexpected Get(server.timeout): %+v, err: %v", entry, err)
	}

	entry, err = cfg.Get("defaults.expiry_days")
	if err != nil || entry.Value != "7" || entry.Type != "int" {
		t.Fatalf("unexpected Get(defaults.expiry_days): %+v, err: %v", entry, err)
	}

	entry, err = cfg.Get("ui.color")
	if err != nil || entry.Value != "true" || entry.Type != "bool" {
		t.Fatalf("unexpected Get(ui.color): %+v, err: %v", entry, err)
	}

	// 2. Set and Get core keys
	coreTests := []struct {
		setKey   string
		setValue string
		getKey   string
		expValue string
		expType  string
	}{
		{"server.url", "https://custom.orbit.io", "server.url", "https://custom.orbit.io", "string"},
		{"server", "https://custom2.orbit.io", "server.url", "https://custom2.orbit.io", "string"},
		{"server.timeout", "45s", "server.timeout", "45s", "duration"},
		{"staff.url", "https://custom.staff.dev", "staff.url", "https://custom.staff.dev", "string"},
		{"staff", "https://custom2.staff.dev", "staff.url", "https://custom2.staff.dev", "string"},
		{"assets.bucket", "my-r2-bucket", "assets.bucket", "my-r2-bucket", "string"},
		{"assets.endpoint", "https://r2.cloudflare.com", "assets.endpoint", "https://r2.cloudflare.com", "string"},
		{"assets.auto_pull", "false", "assets.auto_pull", "false", "bool"},
		{"assets.autopull", "true", "assets.auto_pull", "true", "bool"},
		{"defaults.scope", "org", "defaults.scope", "org", "string"},
		{"defaults.expiry_days", "14", "defaults.expiry_days", "14", "int"},
		{"defaults.expirydays", "30", "defaults.expiry_days", "30", "int"},
		{"ui.color", "false", "ui.color", "false", "bool"},
		{"ui.output", "json", "ui.output", "json", "string"},
	}

	for _, tc := range coreTests {
		if err := cfg.Set(tc.setKey, tc.setValue); err != nil {
			t.Fatalf("Set(%q, %q) failed: %v", tc.setKey, tc.setValue, err)
		}
		got, err := cfg.Get(tc.getKey)
		if err != nil {
			t.Fatalf("Get(%q) failed: %v", tc.getKey, err)
		}
		if got.Value != tc.expValue {
			t.Errorf("Get(%q).Value = %q, expected %q", tc.getKey, got.Value, tc.expValue)
		}
		if got.Type != tc.expType {
			t.Errorf("Get(%q).Type = %q, expected %q", tc.getKey, got.Type, tc.expType)
		}
	}

	// 3. Set and Get custom keys
	if err := cfg.Set("custom.feature_flag", "true"); err != nil {
		t.Fatalf("Set(custom.feature_flag) failed: %v", err)
	}
	got, err := cfg.Get("custom.feature_flag")
	if err != nil || got.Value != "true" || got.Type != "bool" {
		t.Errorf("Get(custom.feature_flag) = %+v, err: %v", got, err)
	}

	if err := cfg.Set("custom.r2.bucket", "my-bucket"); err != nil {
		t.Fatalf("Set(custom.r2.bucket) failed: %v", err)
	}
	got, err = cfg.Get("custom.r2.bucket")
	if err != nil || got.Value != "my-bucket" || got.Type != "string" {
		t.Errorf("Get(custom.r2.bucket) = %+v, err: %v", got, err)
	}

	if err := cfg.Set("custom.deep.nested.port", "10050"); err != nil {
		t.Fatalf("Set(custom.deep.nested.port) failed: %v", err)
	}
	got, err = cfg.Get("custom.deep.nested.port")
	if err != nil || got.Value != "10050" || got.Type != "int" {
		t.Errorf("Get(custom.deep.nested.port) = %+v, err: %v", got, err)
	}

	// 4. Unset core keys resets to default
	if err := cfg.Unset("server.url"); err != nil {
		t.Fatalf("Unset(server.url) failed: %v", err)
	}
	got, err = cfg.Get("server.url")
	if err != nil || got.Value != DefaultServerURL {
		t.Errorf("expected server.url reset to %q, got %q (err: %v)", DefaultServerURL, got.Value, err)
	}

	if err := cfg.Unset("defaults.expiry_days"); err != nil {
		t.Fatalf("Unset(defaults.expiry_days) failed: %v", err)
	}
	got, err = cfg.Get("defaults.expiry_days")
	if err != nil || got.Value != "7" {
		t.Errorf("expected defaults.expiry_days reset to '7', got %q", got.Value)
	}

	if err := cfg.Unset("ui.color"); err != nil {
		t.Fatalf("Unset(ui.color) failed: %v", err)
	}
	got, err = cfg.Get("ui.color")
	if err != nil || got.Value != "true" {
		t.Errorf("expected ui.color reset to 'true', got %q", got.Value)
	}

	// 5. Unset custom keys removes them
	if err := cfg.Unset("custom.r2.bucket"); err != nil {
		t.Fatalf("Unset(custom.r2.bucket) failed: %v", err)
	}
	if _, err := cfg.Get("custom.r2.bucket"); err == nil {
		t.Errorf("expected error getting unset custom.r2.bucket, got nil")
	}

	if err := cfg.Unset("custom.deep.nested.port"); err != nil {
		t.Fatalf("Unset(custom.deep.nested.port) failed: %v", err)
	}
	if _, err := cfg.Get("custom.deep.nested.port"); err == nil {
		t.Errorf("expected error getting unset custom.deep.nested.port, got nil")
	}

	// Unset on non-existent custom key should be idempotent
	if err := cfg.Unset("custom.nonexistent"); err != nil {
		t.Errorf("Unset(custom.nonexistent) returned error: %v", err)
	}

	// 6. Error cases
	if _, err := cfg.Get("nonexistent.domain.key"); err == nil {
		t.Errorf("expected error on Get(nonexistent.domain.key)")
	}
	if err := cfg.Set("invalid.domain.key", "val"); err == nil {
		t.Errorf("expected error on Set(invalid.domain.key)")
	}
	if err := cfg.Set("server.timeout", "not-a-duration"); err == nil {
		t.Errorf("expected error on invalid duration")
	}
	if err := cfg.Set("ui.color", "not-a-bool"); err == nil {
		t.Errorf("expected error on invalid boolean")
	}
	if err := cfg.Set("defaults.expiry_days", "-10"); err == nil {
		t.Errorf("expected error on negative expiry_days")
	}
	if err := cfg.Set("ui.output", "invalid-output"); err == nil {
		t.Errorf("expected error on invalid ui.output")
	}
	if err := cfg.Unset("invalid.domain.key"); err == nil {
		t.Errorf("expected error on Unset(invalid.domain.key)")
	}
}

func TestSmartTypeInference(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expType     string
		expValue    string
		expRawCheck func(t *testing.T, raw interface{})
	}{
		{
			name:     "bool true lowercase",
			input:    "true",
			expType:  "bool",
			expValue: "true",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if b, ok := raw.(bool); !ok || !b {
					t.Errorf("expected raw bool true, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "bool false lowercase",
			input:    "false",
			expType:  "bool",
			expValue: "false",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if b, ok := raw.(bool); !ok || b {
					t.Errorf("expected raw bool false, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "bool TRUE uppercase",
			input:    "TRUE",
			expType:  "bool",
			expValue: "true",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if b, ok := raw.(bool); !ok || !b {
					t.Errorf("expected raw bool true, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "bool False titlecase",
			input:    "False",
			expType:  "bool",
			expValue: "false",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if b, ok := raw.(bool); !ok || b {
					t.Errorf("expected raw bool false, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "positive int",
			input:    "123",
			expType:  "int",
			expValue: "123",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if i, ok := raw.(int); !ok || i != 123 {
					t.Errorf("expected raw int 123, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "negative int",
			input:    "-42",
			expType:  "int",
			expValue: "-42",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if i, ok := raw.(int); !ok || i != -42 {
					t.Errorf("expected raw int -42, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "zero int",
			input:    "0",
			expType:  "int",
			expValue: "0",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if i, ok := raw.(int); !ok || i != 0 {
					t.Errorf("expected raw int 0, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "positive float",
			input:    "3.14",
			expType:  "float",
			expValue: "3.14",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if f, ok := raw.(float64); !ok || f != 3.14 {
					t.Errorf("expected raw float64 3.14, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "negative float",
			input:    "-0.005",
			expType:  "float",
			expValue: "-0.005",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if f, ok := raw.(float64); !ok || f != -0.005 {
					t.Errorf("expected raw float64 -0.005, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "plain string",
			input:    "hello world",
			expType:  "string",
			expValue: "hello world",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if s, ok := raw.(string); !ok || s != "hello world" {
					t.Errorf("expected raw string 'hello world', got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "url string",
			input:    "https://custom.api.dev",
			expType:  "string",
			expValue: "https://custom.api.dev",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if s, ok := raw.(string); !ok || s != "https://custom.api.dev" {
					t.Errorf("expected raw string, got %T (%v)", raw, raw)
				}
			},
		},
		{
			name:     "duration-like custom string",
			input:    "15s",
			expType:  "string",
			expValue: "15s",
			expRawCheck: func(t *testing.T, raw interface{}) {
				if s, ok := raw.(string); !ok || s != "15s" {
					t.Errorf("expected raw string '15s', got %T (%v)", raw, raw)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			if err := cfg.Set("custom.item", tc.input); err != nil {
				t.Fatalf("Set(custom.item, %q) failed: %v", tc.input, err)
			}
			entry, err := cfg.Get("custom.item")
			if err != nil {
				t.Fatalf("Get(custom.item) failed: %v", err)
			}
			if entry.Value != tc.expValue {
				t.Errorf("Value = %q, expected %q", entry.Value, tc.expValue)
			}
			if entry.Type != tc.expType {
				t.Errorf("Type = %q, expected %q", entry.Type, tc.expType)
			}
			tc.expRawCheck(t, cfg.Custom["item"])
		})
	}
}

func TestSchemaValidation(t *testing.T) {
	t.Run("valid default config", func(t *testing.T) {
		cfg := DefaultConfig()
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected DefaultConfig() to pass validation, got: %v", err)
		}
	})

	t.Run("valid modified config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Server.URL = "http://localhost:8080"
		cfg.Staff.URL = "https://staff.internal"
		cfg.Assets.Bucket = "custom-bucket"
		cfg.Defaults.ExpiryDays = 30
		cfg.UI.Output = "json"
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected modified valid config to pass validation, got: %v", err)
		}
	})

	invalidCases := []struct {
		name   string
		mutate func(c *Config)
	}{
		{"empty server.url", func(c *Config) { c.Server.URL = "" }},
		{"invalid server.url scheme", func(c *Config) { c.Server.URL = "ftp://orbit.manova.space" }},
		{"malformed server.url", func(c *Config) { c.Server.URL = "not a valid url" }},
		{"empty server.url host", func(c *Config) { c.Server.URL = "http://" }},
		{"empty staff.url", func(c *Config) { c.Staff.URL = "" }},
		{"invalid staff.url scheme", func(c *Config) { c.Staff.URL = "ftp://staff.dev" }},
		{"malformed staff.url", func(c *Config) { c.Staff.URL = "://invalid-url" }},
		{"empty staff.url host", func(c *Config) { c.Staff.URL = "https://" }},
		{"empty assets.bucket", func(c *Config) { c.Assets.Bucket = "" }},
		{"zero defaults.expiry_days", func(c *Config) { c.Defaults.ExpiryDays = 0 }},
		{"negative defaults.expiry_days", func(c *Config) { c.Defaults.ExpiryDays = -5 }},
		{"empty ui.output", func(c *Config) { c.UI.Output = "" }},
		{"invalid ui.output format", func(c *Config) { c.UI.Output = "xml" }},
		{"unknown ui.output format", func(c *Config) { c.UI.Output = "csv" }},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Errorf("expected validation error for case %q, got nil", tc.name)
			}
		})
	}
}

func TestEntriesFlattening(t *testing.T) {
	t.Run("default config entries", func(t *testing.T) {
		cfg := DefaultConfig()
		entries := cfg.Entries()

		if len(entries) != 10 {
			t.Fatalf("expected 10 core entries, got %d", len(entries))
		}

		entryMap := make(map[string]ConfigEntry)
		for _, e := range entries {
			entryMap[e.Key] = e
			if e.Source != SourceUserFile {
				t.Errorf("entry %s has unexpected source %s", e.Key, e.Source)
			}
			if e.Value == "" && e.Key != "assets.endpoint" {
				t.Errorf("entry %s has unexpected empty value", e.Key)
			}
		}

		expectedKeys := []string{
			"server.url",
			"server.timeout",
			"staff.url",
			"assets.bucket",
			"assets.endpoint",
			"assets.auto_pull",
			"defaults.scope",
			"defaults.expiry_days",
			"ui.color",
			"ui.output",
		}

		for _, k := range expectedKeys {
			if _, exists := entryMap[k]; !exists {
				t.Errorf("expected entry key %q in Entries()", k)
			}
		}
	})

	t.Run("config with custom entries", func(t *testing.T) {
		cfg := DefaultConfig()
		if err := cfg.Set("custom.feature_flag", "true"); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		if err := cfg.Set("custom.r2.bucket", "my-bucket"); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		if err := cfg.Set("custom.deep.nested.port", "8080"); err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		entries := cfg.Entries()
		if len(entries) != 13 {
			t.Fatalf("expected 13 total entries (10 core + 3 custom), got %d: %+v", len(entries), entries)
		}

		entryMap := make(map[string]ConfigEntry)
		for _, e := range entries {
			entryMap[e.Key] = e
		}

		// Verify custom entries
		ff, ok := entryMap["custom.feature_flag"]
		if !ok || ff.Value != "true" || ff.Type != "bool" {
			t.Errorf("custom.feature_flag mismatch: %+v", ff)
		}

		r2, ok := entryMap["custom.r2.bucket"]
		if !ok || r2.Value != "my-bucket" || r2.Type != "string" {
			t.Errorf("custom.r2.bucket mismatch: %+v", r2)
		}

		port, ok := entryMap["custom.deep.nested.port"]
		if !ok || port.Value != "8080" || port.Type != "int" {
			t.Errorf("custom.deep.nested.port mismatch: %+v", port)
		}
	})
}

func TestPrecedenceResolution(t *testing.T) {
	// Helper to find entry by key
	findEntry := func(entries []ConfigEntry, key string) (ConfigEntry, bool) {
		for _, e := range entries {
			if e.Key == key {
				return e, true
			}
		}
		return ConfigEntry{}, false
	}

	t.Run("Tier 1 - Canonical Defaults", func(t *testing.T) {
		nonExistentFile := filepath.Join(t.TempDir(), "nonexistent.yaml")
		res, err := Resolve(ResolveOptions{ConfigPath: nonExistentFile})
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if res == nil || res.Config == nil {
			t.Fatalf("expected non-nil ResolvedConfig and Config")
		}

		if res.Config.Server.URL != DefaultServerURL {
			t.Errorf("expected server url %s, got %s", DefaultServerURL, res.Config.Server.URL)
		}
		if res.Config.Staff.URL != DefaultStaffURL {
			t.Errorf("expected staff url %s, got %s", DefaultStaffURL, res.Config.Staff.URL)
		}
		if res.Config.Assets.Bucket != DefaultAssetBucket {
			t.Errorf("expected assets bucket %s, got %s", DefaultAssetBucket, res.Config.Assets.Bucket)
		}
		if res.Config.Defaults.Scope != DefaultScope {
			t.Errorf("expected defaults scope %s, got %s", DefaultScope, res.Config.Defaults.Scope)
		}
		if res.Config.Defaults.ExpiryDays != DefaultExpiryDays {
			t.Errorf("expected defaults expiry days %d, got %d", DefaultExpiryDays, res.Config.Defaults.ExpiryDays)
		}
		if res.Config.UI.Output != DefaultUIOutput {
			t.Errorf("expected ui output %s, got %s", DefaultUIOutput, res.Config.UI.Output)
		}

		entries := res.ListEntries()
		if len(entries) != 10 {
			t.Fatalf("expected 10 default entries, got %d", len(entries))
		}

		for _, e := range entries {
			if e.Source != SourceDefault {
				t.Errorf("entry %s has unexpected source %s, expected %s", e.Key, e.Source, SourceDefault)
			}
			if e.SourceRef != "default" {
				t.Errorf("entry %s has unexpected source ref %s, expected 'default'", e.Key, e.SourceRef)
			}
		}
	})

	t.Run("Tier 2 - User Config File overrides Defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		fileContent := `version: 2
server:
  url: https://file.orbit.space
defaults:
  expiry_days: 14
custom:
  feature_tier: beta
`
		if err := os.WriteFile(cfgPath, []byte(fileContent), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		res, err := Resolve(ResolveOptions{ConfigPath: cfgPath})
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}

		// Overridden in file
		if res.Config.Server.URL != "https://file.orbit.space" {
			t.Errorf("expected server url https://file.orbit.space, got %s", res.Config.Server.URL)
		}
		if res.Config.Defaults.ExpiryDays != 14 {
			t.Errorf("expected defaults expiry days 14, got %d", res.Config.Defaults.ExpiryDays)
		}

		// Default retained
		if res.Config.Staff.URL != DefaultStaffURL {
			t.Errorf("expected default staff url %s, got %s", DefaultStaffURL, res.Config.Staff.URL)
		}

		entries := res.ListEntries()
		serverEntry, ok := findEntry(entries, "server.url")
		if !ok || serverEntry.Source != SourceUserFile || serverEntry.SourceRef != cfgPath {
			t.Errorf("server.url entry mismatch: %+v (expected SourceUserFile, ref %s)", serverEntry, cfgPath)
		}

		expiryEntry, ok := findEntry(entries, "defaults.expiry_days")
		if !ok || expiryEntry.Source != SourceUserFile || expiryEntry.SourceRef != cfgPath {
			t.Errorf("defaults.expiry_days entry mismatch: %+v", expiryEntry)
		}

		staffEntry, ok := findEntry(entries, "staff.url")
		if !ok || staffEntry.Source != SourceDefault || staffEntry.SourceRef != "default" {
			t.Errorf("staff.url entry mismatch: %+v (expected SourceDefault)", staffEntry)
		}

		customEntry, ok := findEntry(entries, "custom.feature_tier")
		if !ok || customEntry.Source != SourceUserFile || customEntry.Value != "beta" {
			t.Errorf("custom.feature_tier entry mismatch: %+v", customEntry)
		}
	})

	t.Run("Tier 3 - Environment Variables override User File and Defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		fileContent := `version: 2
server:
  url: https://file.orbit.space
  timeout: 20s
defaults:
  expiry_days: 14
`
		if err := os.WriteFile(cfgPath, []byte(fileContent), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		t.Setenv("ORBIT_SERVER", "https://env.orbit.space")
		t.Setenv("ORBIT_SERVER_TIMEOUT", "30s")
		t.Setenv("ORBIT_STAFF_URL", "https://env.staff.space")
		t.Setenv("ORBIT_ASSETS_BUCKET", "env-bucket")
		t.Setenv("ORBIT_ASSETS_ENDPOINT", "https://env.r2.endpoint")
		t.Setenv("ORBIT_ASSETS_AUTO_PULL", "false")
		t.Setenv("ORBIT_DEFAULTS_SCOPE", "org-scope")
		t.Setenv("ORBIT_DEFAULTS_EXPIRY_DAYS", "21")
		t.Setenv("ORBIT_UI_COLOR", "false")
		t.Setenv("ORBIT_UI_OUTPUT", "json")

		res, err := Resolve(ResolveOptions{ConfigPath: cfgPath})
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}

		if res.Config.Server.URL != "https://env.orbit.space" {
			t.Errorf("expected server url https://env.orbit.space, got %s", res.Config.Server.URL)
		}
		if res.Config.Server.Timeout != 30*time.Second {
			t.Errorf("expected server timeout 30s, got %v", res.Config.Server.Timeout)
		}
		if res.Config.Staff.URL != "https://env.staff.space" {
			t.Errorf("expected staff url https://env.staff.space, got %s", res.Config.Staff.URL)
		}
		if res.Config.Assets.Bucket != "env-bucket" {
			t.Errorf("expected assets bucket env-bucket, got %s", res.Config.Assets.Bucket)
		}
		if res.Config.Assets.Endpoint != "https://env.r2.endpoint" {
			t.Errorf("expected assets endpoint https://env.r2.endpoint, got %s", res.Config.Assets.Endpoint)
		}
		if res.Config.Assets.AutoPull != false {
			t.Errorf("expected assets auto_pull false, got %v", res.Config.Assets.AutoPull)
		}
		if res.Config.Defaults.Scope != "org-scope" {
			t.Errorf("expected defaults scope org-scope, got %s", res.Config.Defaults.Scope)
		}
		if res.Config.Defaults.ExpiryDays != 21 {
			t.Errorf("expected defaults expiry days 21, got %d", res.Config.Defaults.ExpiryDays)
		}
		if res.Config.UI.Color != false {
			t.Errorf("expected ui color false, got %v", res.Config.UI.Color)
		}
		if res.Config.UI.Output != "json" {
			t.Errorf("expected ui output json, got %s", res.Config.UI.Output)
		}

		expectedEnvSources := map[string]string{
			"server.url":           "$ORBIT_SERVER",
			"server.timeout":       "$ORBIT_SERVER_TIMEOUT",
			"staff.url":            "$ORBIT_STAFF_URL",
			"assets.bucket":        "$ORBIT_ASSETS_BUCKET",
			"assets.endpoint":      "$ORBIT_ASSETS_ENDPOINT",
			"assets.auto_pull":     "$ORBIT_ASSETS_AUTO_PULL",
			"defaults.scope":       "$ORBIT_DEFAULTS_SCOPE",
			"defaults.expiry_days": "$ORBIT_DEFAULTS_EXPIRY_DAYS",
			"ui.color":             "$ORBIT_UI_COLOR",
			"ui.output":            "$ORBIT_UI_OUTPUT",
		}

		entries := res.ListEntries()
		for key, expectedRef := range expectedEnvSources {
			entry, ok := findEntry(entries, key)
			if !ok {
				t.Errorf("missing entry %s", key)
				continue
			}
			if entry.Source != SourceEnv {
				t.Errorf("entry %s has source %s, expected %s", key, entry.Source, SourceEnv)
			}
			if entry.SourceRef != expectedRef {
				t.Errorf("entry %s has source ref %s, expected %s", key, entry.SourceRef, expectedRef)
			}
		}
	})

	t.Run("Tier 3 - ORBIT_STAFF fallback", func(t *testing.T) {
		t.Setenv("ORBIT_STAFF_URL", "")
		t.Setenv("ORBIT_STAFF", "https://fallback.staff.space")

		res, err := Resolve(ResolveOptions{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if res.Config.Staff.URL != "https://fallback.staff.space" {
			t.Errorf("expected fallback staff url https://fallback.staff.space, got %s", res.Config.Staff.URL)
		}
		entry, ok := findEntry(res.ListEntries(), "staff.url")
		if !ok || entry.Source != SourceEnv || entry.SourceRef != "$ORBIT_STAFF" {
			t.Errorf("staff.url entry mismatch: %+v (expected SourceEnv, ref $ORBIT_STAFF)", entry)
		}
	})

	t.Run("Tier 4 - Explicit CLI Flags override Env, File, and Defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		fileContent := `version: 2
server:
  url: https://file.orbit.space
staff:
  url: https://file.staff.space
assets:
  bucket: file-bucket
defaults:
  scope: file-scope
`
		if err := os.WriteFile(cfgPath, []byte(fileContent), 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		t.Setenv("ORBIT_SERVER", "https://env.orbit.space")
		t.Setenv("ORBIT_STAFF_URL", "https://env.staff.space")
		t.Setenv("ORBIT_ASSETS_BUCKET", "env-bucket")
		t.Setenv("ORBIT_DEFAULTS_SCOPE", "env-scope")
		t.Setenv("ORBIT_UI_OUTPUT", "yaml") // Should remain SourceEnv

		opts := ResolveOptions{
			ConfigPath:       cfgPath,
			ServerFlag:       "https://flag.orbit.space",
			StaffURLFlag:     "https://flag.staff.space",
			AssetsBucketFlag: "flag-bucket",
			ScopeFlag:        "flag-scope",
		}

		res, err := Resolve(opts)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}

		if res.Config.Server.URL != "https://flag.orbit.space" {
			t.Errorf("expected server url https://flag.orbit.space, got %s", res.Config.Server.URL)
		}
		if res.Config.Staff.URL != "https://flag.staff.space" {
			t.Errorf("expected staff url https://flag.staff.space, got %s", res.Config.Staff.URL)
		}
		if res.Config.Assets.Bucket != "flag-bucket" {
			t.Errorf("expected assets bucket flag-bucket, got %s", res.Config.Assets.Bucket)
		}
		if res.Config.Defaults.Scope != "flag-scope" {
			t.Errorf("expected defaults scope flag-scope, got %s", res.Config.Defaults.Scope)
		}
		if res.Config.UI.Output != "yaml" {
			t.Errorf("expected ui output yaml from env, got %s", res.Config.UI.Output)
		}

		entries := res.ListEntries()
		expectedFlagSources := map[string]string{
			"server.url":      "--server",
			"staff.url":       "--staff-url",
			"assets.bucket":   "--bucket",
			"defaults.scope":  "--scope",
		}

		for key, expectedRef := range expectedFlagSources {
			entry, ok := findEntry(entries, key)
			if !ok {
				t.Errorf("missing entry %s", key)
				continue
			}
			if entry.Source != SourceFlag {
				t.Errorf("entry %s has source %s, expected %s", key, entry.Source, SourceFlag)
			}
			if entry.SourceRef != expectedRef {
				t.Errorf("entry %s has source ref %s, expected %s", key, entry.SourceRef, expectedRef)
			}
		}

		// Verify ui.output remained SourceEnv
		uiEntry, ok := findEntry(entries, "ui.output")
		if !ok || uiEntry.Source != SourceEnv || uiEntry.SourceRef != "$ORBIT_UI_OUTPUT" {
			t.Errorf("ui.output entry mismatch: %+v (expected SourceEnv, ref $ORBIT_UI_OUTPUT)", uiEntry)
		}
	})
}

func TestResolveValidation(t *testing.T) {
	t.Run("invalid server url from env", func(t *testing.T) {
		t.Setenv("ORBIT_SERVER", "invalid url scheme")
		_, err := Resolve(ResolveOptions{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
		if err == nil {
			t.Errorf("expected validation error for invalid ORBIT_SERVER, got nil")
		}
	})

	t.Run("invalid staff url from flag", func(t *testing.T) {
		opts := ResolveOptions{
			ConfigPath:   filepath.Join(t.TempDir(), "config.yaml"),
			StaffURLFlag: "ftp://invalid-scheme",
		}
		_, err := Resolve(opts)
		if err == nil {
			t.Errorf("expected validation error for invalid StaffURLFlag, got nil")
		}
	})

	t.Run("invalid server timeout from env", func(t *testing.T) {
		t.Setenv("ORBIT_SERVER_TIMEOUT", "invalid-duration")
		_, err := Resolve(ResolveOptions{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
		if err == nil {
			t.Errorf("expected error for invalid ORBIT_SERVER_TIMEOUT, got nil")
		}
	})

	t.Run("invalid assets auto pull from env", func(t *testing.T) {
		t.Setenv("ORBIT_ASSETS_AUTO_PULL", "notabool")
		_, err := Resolve(ResolveOptions{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
		if err == nil {
			t.Errorf("expected error for invalid ORBIT_ASSETS_AUTO_PULL, got nil")
		}
	})

	t.Run("invalid expiry days from env", func(t *testing.T) {
		t.Setenv("ORBIT_DEFAULTS_EXPIRY_DAYS", "-5")
		_, err := Resolve(ResolveOptions{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
		if err == nil {
			t.Errorf("expected error for negative ORBIT_DEFAULTS_EXPIRY_DAYS, got nil")
		}
	})

	t.Run("invalid ui color from env", func(t *testing.T) {
		t.Setenv("ORBIT_UI_COLOR", "notabool")
		_, err := Resolve(ResolveOptions{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
		if err == nil {
			t.Errorf("expected error for invalid ORBIT_UI_COLOR, got nil")
		}
	})

	t.Run("invalid ui output format from env", func(t *testing.T) {
		t.Setenv("ORBIT_UI_OUTPUT", "xml")
		_, err := Resolve(ResolveOptions{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
		if err == nil {
			t.Errorf("expected error for invalid ORBIT_UI_OUTPUT, got nil")
		}
	})
}


