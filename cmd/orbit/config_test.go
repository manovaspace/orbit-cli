package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/config"
	"gopkg.in/yaml.v3"
)

func execConfig(args ...string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(append([]string{"config"}, args...))
	err := cmd.Execute()
	return buf, err
}

func TestConfigShowAndInit(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// 1. config init on fresh path
	buf, err := execConfig("init", "--config", cfgPath)
	if err != nil {
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

	// 2. config init on existing file without --force
	buf, err = execConfig("init", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config init on existing file failed: %v", err)
	}
	if !strings.Contains(buf.String(), "already exists") {
		t.Errorf("expected already exists message, got: %s", buf.String())
	}

	// 3. config init with --force
	buf, err = execConfig("init", "--config", cfgPath, "--force")
	if err != nil {
		t.Fatalf("config init --force failed: %v", err)
	}
	if !strings.Contains(buf.String(), "initialized") {
		t.Errorf("expected initialized message with --force, got: %s", buf.String())
	}

	// 4. config show (default YAML format)
	buf, err = execConfig("show", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config show failed: %v", err)
	}
	showOut := buf.String()
	if !strings.Contains(showOut, "https://orbit.manova.space") {
		t.Errorf("expected default server url in show output, got: %s", showOut)
	}

	// 5. config show --format json
	buf, err = execConfig("show", "--config", cfgPath, "--format", "json")
	if err != nil {
		t.Fatalf("config show --format json failed: %v", err)
	}
	var jsonCfg config.Config
	if err := json.Unmarshal(buf.Bytes(), &jsonCfg); err != nil {
		t.Fatalf("failed to parse JSON from config show: %v\nOutput: %s", err, buf.String())
	}
	if jsonCfg.Server.URL != "https://orbit.manova.space" {
		t.Errorf("expected server url in JSON, got %s", jsonCfg.Server.URL)
	}
}

func TestConfigGetRaw(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Initialize config
	if _, err := execConfig("init", "--config", cfgPath); err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	// 1. Standard get (has newline)
	buf, err := execConfig("get", "server.url", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config get server.url failed: %v", err)
	}
	if buf.String() != "https://orbit.manova.space\n" {
		t.Errorf("expected 'https://orbit.manova.space\\n', got %q", buf.String())
	}

	// 2. Get with --raw (no trailing newline)
	buf, err = execConfig("get", "server.url", "--raw", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config get --raw failed: %v", err)
	}
	if buf.String() != "https://orbit.manova.space" {
		t.Errorf("expected exact 'https://orbit.manova.space' without newline, got %q", buf.String())
	}
}

func TestConfigSetSecretWarning(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	if _, err := execConfig("init", "--config", cfgPath); err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	// 1. Set normal key (no warning)
	buf, err := execConfig("set", "defaults.scope", "custom-scope", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config set defaults.scope failed: %v", err)
	}
	if strings.Contains(buf.String(), "resembles a secret") {
		t.Errorf("unexpected secret warning for defaults.scope: %s", buf.String())
	}

	// 2. Set key with 'secret' in name -> must emit warning
	buf, err = execConfig("set", "custom.jwt_secret", "supersecret123", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config set custom.jwt_secret failed: %v", err)
	}
	if !strings.Contains(buf.String(), "resembles a secret. Storing secrets in config.yaml is discouraged.") {
		t.Errorf("expected secret warning for custom.jwt_secret, got: %s", buf.String())
	}

	// 3. Set key with 'token' in name -> must emit warning
	buf, err = execConfig("set", "custom.api_token", "tok_xyz987", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config set custom.api_token failed: %v", err)
	}
	if !strings.Contains(buf.String(), "resembles a secret. Storing secrets in config.yaml is discouraged.") {
		t.Errorf("expected secret warning for custom.api_token, got: %s", buf.String())
	}

	// 4. Set key with 'pass' in name -> must emit warning
	buf, err = execConfig("set", "custom.db_password", "p@ssword", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config set custom.db_password failed: %v", err)
	}
	if !strings.Contains(buf.String(), "resembles a secret. Storing secrets in config.yaml is discouraged.") {
		t.Errorf("expected secret warning for custom.db_password, got: %s", buf.String())
	}

	// 5. Verify values persisted correctly
	buf, err = execConfig("get", "custom.jwt_secret", "--raw", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config get custom.jwt_secret failed: %v", err)
	}
	if buf.String() != "supersecret123" {
		t.Errorf("expected 'supersecret123', got %q", buf.String())
	}
}

func TestConfigSetASTCommentPreservation(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `# Top Header Comment
version: 2

# Server endpoint settings
server:
  # The primary edge cluster URL
  url: https://orbit.manova.space
  timeout: 15s

# Workspace defaults
defaults:
  scope: all
  expiry_days: 7
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0600); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// Mutate server.url via orbit config set
	if _, err := execConfig("set", "server.url", "https://custom-edge.manova.space", "--config", cfgPath); err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read mutated config: %v", err)
	}
	content := string(data)

	// Comments should be preserved
	if !strings.Contains(content, "# Top Header Comment") {
		t.Errorf("expected '# Top Header Comment' preserved in AST, got:\n%s", content)
	}
	if !strings.Contains(content, "# The primary edge cluster URL") {
		t.Errorf("expected '# The primary edge cluster URL' preserved in AST, got:\n%s", content)
	}
	if !strings.Contains(content, "https://custom-edge.manova.space") {
		t.Errorf("expected mutated URL in config, got:\n%s", content)
	}
}

func TestConfigUnsetCommand(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	if _, err := execConfig("init", "--config", cfgPath); err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	// 1. Set a custom key
	if _, err := execConfig("set", "custom.debug_mode", "true", "--config", cfgPath); err != nil {
		t.Fatalf("config set custom.debug_mode failed: %v", err)
	}

	// 2. Unset the custom key
	buf, err := execConfig("unset", "custom.debug_mode", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config unset custom.debug_mode failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Unset custom.debug_mode") {
		t.Errorf("expected unset confirmation, got: %s", buf.String())
	}

	// Verify key no longer exists
	if _, err := execConfig("get", "custom.debug_mode", "--config", cfgPath); err == nil {
		t.Fatalf("expected error getting unset custom key, got nil")
	}

	// 3. Unset a core domain property (resets or deletes node)
	buf, err = execConfig("unset", "server.url", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config unset server.url failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Unset server.url") {
		t.Errorf("expected unset confirmation for server.url, got: %s", buf.String())
	}
}

func TestConfigListCommand(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// Set an environment variable for testing source resolution
	t.Setenv("ORBIT_DEFAULTS_SCOPE", "env-scoped")

	// 1. Table format (default)
	buf, err := execConfig("list", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config list failed: %v", err)
	}
	tableOut := buf.String()
	if !strings.Contains(tableOut, "KEY") || !strings.Contains(tableOut, "VALUE") || !strings.Contains(tableOut, "SOURCE") {
		t.Errorf("expected table header columns in list output, got:\n%s", tableOut)
	}
	if !strings.Contains(tableOut, "server.url") {
		t.Errorf("expected server.url in table output, got:\n%s", tableOut)
	}
	if !strings.Contains(tableOut, "defaults.scope") {
		t.Errorf("expected defaults.scope in table output, got:\n%s", tableOut)
	}
	if !strings.Contains(tableOut, "env ($ORBIT_DEFAULTS_SCOPE)") && !strings.Contains(tableOut, "env") {
		t.Errorf("expected env source indicator in table output, got:\n%s", tableOut)
	}

	// 2. JSON format
	buf, err = execConfig("list", "--config", cfgPath, "--format", "json")
	if err != nil {
		t.Fatalf("config list --format json failed: %v", err)
	}
	var jsonEntries []config.ConfigEntry
	if err := json.Unmarshal(buf.Bytes(), &jsonEntries); err != nil {
		t.Fatalf("failed to parse JSON from config list: %v\nOutput: %s", err, buf.String())
	}
	if len(jsonEntries) == 0 {
		t.Fatalf("expected non-empty entries list in JSON")
	}

	var foundScope bool
	for _, e := range jsonEntries {
		if e.Key == "defaults.scope" {
			foundScope = true
			if e.Value != "env-scoped" {
				t.Errorf("expected defaults.scope = 'env-scoped', got %q", e.Value)
			}
			if e.Source != config.SourceEnv {
				t.Errorf("expected source == env, got %q", e.Source)
			}
		}
	}
	if !foundScope {
		t.Errorf("expected defaults.scope entry in JSON list")
	}

	// 3. YAML format
	buf, err = execConfig("list", "--config", cfgPath, "--format", "yaml")
	if err != nil {
		t.Fatalf("config list --format yaml failed: %v", err)
	}
	var yamlEntries []config.ConfigEntry
	if err := yaml.Unmarshal(buf.Bytes(), &yamlEntries); err != nil {
		t.Fatalf("failed to parse YAML from config list: %v\nOutput: %s", err, buf.String())
	}
	if len(yamlEntries) == 0 {
		t.Fatalf("expected non-empty entries list in YAML")
	}
}

func TestConfigPathCommand(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "custom-path.yaml")

	buf, err := execConfig("path", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config path failed: %v", err)
	}
	if strings.TrimSpace(buf.String()) != cfgPath {
		t.Errorf("expected %q, got %q", cfgPath, strings.TrimSpace(buf.String()))
	}
}

func TestConfigViaRootCmd(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "init", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("orbit config init failed: %v", err)
	}

	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "path", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("orbit config path failed: %v", err)
	}
	if !strings.Contains(buf.String(), cfgPath) {
		t.Errorf("expected config path %s, got %s", cfgPath, buf.String())
	}

	buf.Reset()
	cmd = newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "get", "server.url", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("orbit config get server.url failed: %v", err)
	}
	if !strings.Contains(buf.String(), "https://orbit.manova.space") {
		t.Errorf("expected default server url, got %s", buf.String())
	}
}
