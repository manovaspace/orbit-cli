package osfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/config/domain"
	"gopkg.in/yaml.v3"
)

var DefaultConfig = domain.DefaultConfig

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

func TestLoadAutoTightensPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "insecure-config.yaml")

	cfg := DefaultConfig()
	if err := New().Save(cfgPath, cfg); err != nil {
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
	loaded, err := New().Load(cfgPath)
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

	_, err := New().Load(customPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}

	cfg := DefaultConfig()
	cfg.Server.URL = "https://custom.server.space"
	if err := New().Save(customPath, cfg); err != nil {
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

	loaded, err := New().Load(customPath)
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
  url: https://staff.internal.manova.space

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
	if err := New().SetInFile(cfgPath, "server.url", "https://updated.orbit.manova.space"); err != nil {
		t.Fatalf("SetInFile(server.url) failed: %v", err)
	}

	// 2. Mutate a custom boolean key
	if err := New().SetInFile(cfgPath, "custom.dev_mode", "true"); err != nil {
		t.Fatalf("SetInFile(custom.dev_mode) failed: %v", err)
	}

	// 3. Add a new key under custom
	if err := New().SetInFile(cfgPath, "custom.new_flag", "enabled"); err != nil {
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
	if err := New().SetInFile(cfgPath, "server.url", "https://custom-init.orbit.io"); err != nil {
		t.Fatalf("SetInFile on non-existent file failed: %v", err)
	}

	// Verify file was created and can be loaded
	cfg, err := New().Load(cfgPath)
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
	if err := New().SetInFile(cfgPath, "ui.color", "false"); err != nil {
		t.Fatalf("SetInFile(ui.color) failed: %v", err)
	}
	if err := New().SetInFile(cfgPath, "defaults.expiry_days", "14"); err != nil {
		t.Fatalf("SetInFile(defaults.expiry_days) failed: %v", err)
	}
	if err := New().SetInFile(cfgPath, "server.timeout", "45s"); err != nil {
		t.Fatalf("SetInFile(server.timeout) failed: %v", err)
	}

	cfg, err = New().Load(cfgPath)
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
	if err := New().SetInFile(cfgPath, "custom.deep.nested.feature_flag", "true"); err != nil {
		t.Fatalf("SetInFile(custom.deep.nested.feature_flag) failed: %v", err)
	}
	if err := New().SetInFile(cfgPath, "custom.deep.nested.port", "10050"); err != nil {
		t.Fatalf("SetInFile(custom.deep.nested.port) failed: %v", err)
	}
	if err := New().SetInFile(cfgPath, "custom.deep.nested.endpoint", "https://api.internal"); err != nil {
		t.Fatalf("SetInFile(custom.deep.nested.endpoint) failed: %v", err)
	}

	cfg, err = New().Load(cfgPath)
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
	if err := New().UnsetInFile(cfgPath, "custom.deep.nested.feature_flag"); err != nil {
		t.Fatalf("UnsetInFile(feature_flag) failed: %v", err)
	}

	cfg, err = New().Load(cfgPath)
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
	if err := New().UnsetInFile(cfgPath, "non.existent.key"); err != nil {
		t.Errorf("expected UnsetInFile on non-existent key to succeed without error, got: %v", err)
	}

	// 6. UnsetInFile on non-existent file (idempotent)
	nonExistentFile := filepath.Join(tmpDir, "does-not-exist.yaml")
	if err := New().UnsetInFile(nonExistentFile, "some.key"); err != nil {
		t.Errorf("expected UnsetInFile on non-existent file to succeed or return nil, got: %v", err)
	}
}

func TestAtomicWritePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "nested-dir", "perm-config.yaml")

	// SetInFile creates directory and file with 0600
	if err := New().SetInFile(targetFile, "server.url", "https://orbit.manova.space"); err != nil {
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
	if err := New().SetInFile(targetFile, "ui.color", "false"); err != nil {
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
	if err := New().UnsetInFile(targetFile, "ui.color"); err != nil {
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
