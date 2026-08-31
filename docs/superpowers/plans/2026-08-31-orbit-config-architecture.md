# Orbit CLI Configuration Architecture (`orbit config`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a fast, resilient, comment-preserving, and extensible dot-notation configuration engine for `orbit` (`pkg/config` and `cmd/orbit/config.go`) following the approved v2 design specification.

**Architecture:** A 4-tier configuration precedence engine (Flags > Env Vars > `~/.config/orbit/config.yaml` > Embedded Defaults) with typed core domains (`server`, `staff`, `assets`, `defaults`, `ui`) and dynamic `custom.*` namespaces. File mutations use `yaml.Node` AST manipulation to preserve comments and layout with atomic file writes (POSIX mode `0600`).

**Tech Stack:** Go 1.26, Cobra CLI (`github.com/spf13/cobra`), `gopkg.in/yaml.v3` (`yaml.Node` AST), Charm Lipgloss (`github.com/charmbracelet/lipgloss`).

## Global Constraints

* Toolchain: Go 1.26 standard library and existing dependencies in `go.mod`.
* Configuration File: `$XDG_CONFIG_HOME/orbit/config.yaml` (default `~/.config/orbit/config.yaml`) with strict POSIX mode `0600`.
* Secret Isolation: Zero plaintext SMTP credentials in workstation `Config`. Workstation secrets live in `owner.json` or environment variables.
* No Breaking CLI Changes: Existing commands `orbit config show`, `get`, `set`, `init`, `path` remain functional while gaining `unset`, `list`, `--raw`, and source tracking.
* No Commits unless explicitly requested by the user.

---

### Task 1: Core Types, Canonical Defaults & Path Resolution

**Files:**
- Modify: `orbit/orbit-cli/pkg/config/types.go`
- Modify: `orbit/orbit-cli/pkg/config/config.go:1-118`
- Test: `orbit/orbit-cli/pkg/config/config_test.go`

**Interfaces:**
- Consumes: Standard library (`time`, `os`, `path/filepath`, `strings`).
- Produces: `Config`, `ServerConfig`, `StaffConfig`, `AssetsConfig`, `DefaultsConfig`, `UIConfig`, `Source`, `ConfigEntry`, `DefaultConfig()`, `DefaultConfigPath()`.

- [ ] **Step 1: Write failing unit test for types and default configuration**

```go
// in pkg/config/config_test.go
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Version != 2 {
		t.Fatalf("expected version 2, got %d", cfg.Version)
	}
	if cfg.Server.URL != "https://orbit.manova.space" {
		t.Errorf("expected default server URL, got %s", cfg.Server.URL)
	}
	if cfg.Staff.URL != "https://staff.dev.manova.space" {
		t.Errorf("expected default staff URL, got %s", cfg.Staff.URL)
	}
	if cfg.Assets.Bucket != "orbit-assets" || !cfg.Assets.AutoPull {
		t.Errorf("expected default assets config, got %+v", cfg.Assets)
	}
	if cfg.Defaults.Scope != "all" || cfg.Defaults.ExpiryDays != 7 {
		t.Errorf("expected default defaults config, got %+v", cfg.Defaults)
	}
	if !cfg.UI.Color || cfg.UI.Output != "table" {
		t.Errorf("expected default UI config, got %+v", cfg.UI)
	}
	if cfg.Custom == nil {
		t.Errorf("expected initialized custom map")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("ORBIT_CONFIG", "/custom/path/config.yaml")
	if p := DefaultConfigPath(); p != "/custom/path/config.yaml" {
		t.Errorf("expected /custom/path/config.yaml, got %s", p)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -v ./pkg/config -run "TestDefaultConfig"`
Expected: FAIL due to missing struct fields (`Version`, `Staff`, `Assets`, `UI`, `Custom`).

- [ ] **Step 3: Implement new types and default configuration**

Update `pkg/config/types.go` and `pkg/config/config.go` with:
- `Source` enum (`SourceDefault`, `SourceUserFile`, `SourceEnv`, `SourceFlag`).
- `ConfigEntry` struct (`Key`, `Value`, `Type`, `Source`, `SourceRef`).
- `Config` with `Version: 2`, `ServerConfig`, `StaffConfig`, `AssetsConfig`, `DefaultsConfig`, `UIConfig`, `Custom map[string]interface{}`.
- `DefaultConfig()` initializing all default values.
- `DefaultConfigPath()` resolving `$ORBIT_CONFIG`, `$XDG_CONFIG_HOME/orbit/config.yaml`, or `~/.config/orbit/config.yaml`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/config -run "TestDefaultConfig"`
Expected: PASS.

---

### Task 2: Comment-Preserving `yaml.Node` AST Engine & Atomic File I/O

**Files:**
- Modify: `orbit/orbit-cli/pkg/config/config.go`
- Test: `orbit/orbit-cli/pkg/config/config_test.go`

**Interfaces:**
- Consumes: `gopkg.in/yaml.v3` (`yaml.Node`, `yaml.DocumentNode`, `yaml.MappingNode`, `yaml.ScalarNode`).
- Produces: `SetInFile(filePath, path, value string) error`, `UnsetInFile(filePath, path string) error`, `Load(path string) (*Config, error)`, `Save(path string) error`.

- [ ] **Step 1: Write failing unit tests for comment-preserving mutations**

```go
func TestCommentPreservation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	initialYAML := `# Primary Orbit Developer Configuration
version: 2

# Core server gateway
server:
  url: "https://orbit.manova.space" # Production URL
  timeout: 15s

assets:
  bucket: "orbit-assets"
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0600); err != nil {
		t.Fatal(err)
	}

	// Update server.url via SetInFile
	if err := SetInFile(cfgPath, "server.url", "https://staging.orbit.manova.space"); err != nil {
		t.Fatalf("SetInFile failed: %v", err)
	}

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	raw := string(content)
	if !strings.Contains(raw, "# Primary Orbit Developer Configuration") {
		t.Errorf("lost header comment after mutation")
	}
	if !strings.Contains(raw, "# Production URL") && !strings.Contains(raw, "# Core server gateway") {
		t.Errorf("lost inline or block comment after mutation")
	}
	if !strings.Contains(raw, "https://staging.orbit.manova.space") {
		t.Errorf("value was not updated in YAML file")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -v ./pkg/config -run "TestCommentPreservation"`
Expected: FAIL with `SetInFile` not defined.

- [ ] **Step 3: Implement AST traversal & mutation via `yaml.Node`**

Implement in `pkg/config/config.go`:
- `modifyNode(doc *yaml.Node, parts []string, value string) error`: Traverses mapping nodes, creates intermediate mappings if missing, and updates leaf scalar nodes.
- `deleteNode(doc *yaml.Node, parts []string) error`: Traverses mapping nodes and removes key-value pairs.
- `SetInFile(filePath, path, value string) error`: Parses file to `yaml.Node`, mutates target path, encodes back using atomic write with mode `0600`.
- `UnsetInFile(filePath, path string) error`: Parses file, removes key, writes back atomically.
- `atomicWriteFile(path string, data []byte, perm os.FileMode) error`: Writes to `.tmp-*` in same directory and renames via `os.Rename`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/config -run "TestCommentPreservation"`
Expected: PASS.

---

### Task 3: Extensible Dot-Notation Engine (`Get`, `Set`, `Unset`, `Validate`)

**Files:**
- Modify: `orbit/orbit-cli/pkg/config/config.go`
- Test: `orbit/orbit-cli/pkg/config/config_test.go`

**Interfaces:**
- Consumes: `Config` struct, AST mutator.
- Produces: `(c *Config) Get(path string) (string, error)`, `(c *Config) Set(path, value string) error`, `(c *Config) Unset(path string) error`, `(c *Config) Validate() error`.

- [ ] **Step 1: Write failing unit tests for dot-notation getters, setters, and validation**

```go
func TestDotNotationAccess(t *testing.T) {
	cfg := DefaultConfig()

	// Typed Core Domains
	if err := cfg.Set("server.url", "https://custom.orbit.space"); err != nil {
		t.Fatalf("failed to set server.url: %v", err)
	}
	if val, err := cfg.Get("server.url"); err != nil || val != "https://custom.orbit.space" {
		t.Errorf("expected custom URL, got %s (err: %v)", val, err)
	}

	if err := cfg.Set("assets.auto_pull", "false"); err != nil {
		t.Fatalf("failed to set assets.auto_pull: %v", err)
	}
	if val, err := cfg.Get("assets.auto_pull"); err != nil || val != "false" {
		t.Errorf("expected false, got %s", val)
	}

	// Dynamic Custom Domain
	if err := cfg.Set("custom.dev_flag", "true"); err != nil {
		t.Fatalf("failed to set custom.dev_flag: %v", err)
	}
	if val, err := cfg.Get("custom.dev_flag"); err != nil || val != "true" {
		t.Errorf("expected true, got %s", val)
	}

	// Validation
	if err := cfg.Set("defaults.expiry_days", "-5"); err == nil {
		t.Errorf("expected error for negative expiry days")
	}
	if err := cfg.Set("server.timeout", "invalid-duration"); err == nil {
		t.Errorf("expected error for invalid duration")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -v ./pkg/config -run "TestDotNotationAccess"`
Expected: FAIL due to missing keys (`assets.auto_pull`, `custom.dev_flag`).

- [ ] **Step 3: Implement dynamic dot-notation accessors & validation**

Implement in `pkg/config/config.go`:
- Supported paths:
  - `server.url`, `server.timeout`
  - `staff.url`
  - `assets.bucket`, `assets.endpoint`, `assets.auto_pull`
  - `defaults.scope`, `defaults.expiry_days`
  - `ui.color`, `ui.output`
  - `custom.<key>` (stores in `c.Custom` with automatic type inference: bool, int, float, string)
- `Get(path string)`: returns string formatted representation of the value.
- `Set(path, value string)`: validates input format and sets field on struct and `custom` map.
- `Unset(path string)`: resets core fields to `DefaultConfig()` value or deletes from `Custom`.
- `Validate()`: verifies URLs start with `http://` or `https://`, positive integer ranges, valid `ui.output` (`table`, `json`, `yaml`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/config -run "TestDotNotationAccess"`
Expected: PASS.

---

### Task 4: 4-Tier Precedence Engine (`Resolve`) & Legacy Migration

**Files:**
- Modify: `orbit/orbit-cli/pkg/config/config.go`
- Test: `orbit/orbit-cli/pkg/config/config_test.go`

**Interfaces:**
- Consumes: `ResolveOptions` (`ConfigPath`, `ServerFlag`, `StaffURLFlag`, `AssetsBucketFlag`, `ScopeFlag`).
- Produces: `ResolvedConfig` (`Config`, `Entries []ConfigEntry`), `Resolve(opts ResolveOptions) (*ResolvedConfig, error)`.

- [ ] **Step 1: Write failing unit tests for precedence resolution and legacy migration**

```go
func TestPrecedenceResolution(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	fileYAML := `version: 2
server:
  url: "https://file.orbit.space"
`
	if err := os.WriteFile(cfgPath, []byte(fileYAML), 0600); err != nil {
		t.Fatal(err)
	}

	// 1. File beats default
	res, err := Resolve(ResolveOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.Server.URL != "https://file.orbit.space" {
		t.Errorf("expected file URL, got %s", res.Config.Server.URL)
	}

	// 2. Env beats file
	t.Setenv("ORBIT_SERVER", "https://env.orbit.space")
	res, err = Resolve(ResolveOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.Server.URL != "https://env.orbit.space" {
		t.Errorf("expected env URL, got %s", res.Config.Server.URL)
	}

	// 3. Flag beats env
	res, err = Resolve(ResolveOptions{ConfigPath: cfgPath, ServerFlag: "https://flag.orbit.space"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.Server.URL != "https://flag.orbit.space" {
		t.Errorf("expected flag URL, got %s", res.Config.Server.URL)
	}
}

func TestLegacyMigration(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))

	legacyDir := filepath.Join(homeDir, ".config", "manova")
	_ = os.MkdirAll(legacyDir, 0700)
	_ = os.WriteFile(filepath.Join(legacyDir, "config.yaml"), []byte("version: 1\nserver:\n  url: https://legacy.manova.space\n"), 0600)

	// Loading default path should auto-migrate
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.URL != "https://legacy.manova.space" {
		t.Errorf("expected legacy URL migrated, got %s", cfg.Server.URL)
	}

	// Verify migrated file exists
	orbitCfg := filepath.Join(homeDir, ".config", "orbit", "config.yaml")
	if _, err := os.Stat(orbitCfg); err != nil {
		t.Errorf("expected migrated file at %s", orbitCfg)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -v ./pkg/config -run "TestPrecedenceResolution|TestLegacyMigration"`
Expected: FAIL.

- [ ] **Step 3: Implement `Resolve` precedence engine & auto-migration**

Implement in `pkg/config/config.go`:
- `checkAndMigrateLegacy(targetPath string)`: If `~/.config/orbit/config.yaml` is absent and `~/.config/manova/config.yaml` exists, copy file and set permissions to `0600`.
- `Resolve(opts ResolveOptions) (*ResolvedConfig, error)`:
  - Step 1: Initialize with defaults and tag entries with `SourceDefault`.
  - Step 2: Load file (`opts.ConfigPath` or `DefaultConfigPath()`), apply loaded values, tag modified fields with `SourceUserFile`.
  - Step 3: Check environment variables (`ORBIT_SERVER`, `ORBIT_STAFF_URL`, `ORBIT_ASSETS_BUCKET`, `ORBIT_ASSETS_ENDPOINT`, `ORBIT_DEFAULTS_SCOPE`, `ORBIT_DEFAULTS_EXPIRY_DAYS`, `ORBIT_UI_COLOR`, `ORBIT_UI_OUTPUT`), apply and tag with `SourceEnv` (`source_ref = "$ORBIT_SERVER"`).
  - Step 4: Check explicit flags in `opts`, apply and tag with `SourceFlag`.
  - Returns `ResolvedConfig` with `Config` and `ListEntries() []ConfigEntry`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/config -run "TestPrecedenceResolution|TestLegacyMigration"`
Expected: PASS.

---

### Task 5: Modernize CLI Subcommands (`cmd/orbit/config.go`)

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/config.go`
- Modify: `orbit/orbit-cli/cmd/orbit/config_test.go`

**Interfaces:**
- Consumes: `pkg/config`.
- Produces: Cobra commands `orbit config [show|get|set|unset|list|init|path]`.

- [ ] **Step 1: Write failing CLI tests for `list`, `unset`, `--raw`, and secret warning**

```go
// in cmd/orbit/config_test.go
func TestConfigListCommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "list", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config list failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "server.url") || !strings.Contains(out, "default") {
		t.Errorf("expected server.url and default source in output:\n%s", out)
	}
}

func TestConfigGetRaw(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"config", "get", "server.url", "--raw", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "https://orbit.manova.space" {
		t.Errorf("expected raw string without newline, got %q", buf.String())
	}
}

func TestConfigSetSecretWarning(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"config", "set", "custom.api_secret", "my-secret-token", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "Warning") && !strings.Contains(buf.String(), "Warning") {
		t.Errorf("expected security warning when setting secret-like key")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -v ./cmd/orbit -run "TestConfigListCommand|TestConfigGetRaw|TestConfigSetSecretWarning"`
Expected: FAIL with `list` and `--raw` not recognized.

- [ ] **Step 3: Update `cmd/orbit/config.go` subcommands**

Update `cmd/orbit/config.go`:
- `newConfigGetCmd()`: add `--raw` flag to output untrimmed, non-colorized raw string.
- `newConfigSetCmd()`: check if key contains `pass`, `secret`, `token`, `key`, `auth` and print Lipgloss warning style to stderr. Use `config.SetInFile(cfgPath, key, val)`.
- `newConfigUnsetCmd()`: call `config.UnsetInFile(cfgPath, key)` and print success message.
- `newConfigListCmd()`: call `config.Resolve()`, format table with columns `KEY`, `VALUE`, `TYPE`, `SOURCE` using Lipgloss headers. Support `--format json|yaml|table`.
- `newConfigShowCmd()`: display resolved YAML or JSON config.
- `newConfigInitCmd()` & `newConfigPathCmd()`: retain functionality with mode `0600`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./cmd/orbit -run "TestConfigListCommand|TestConfigGetRaw|TestConfigSetSecretWarning"`
Expected: PASS.

---

### Task 6: Global Root `--config` Flag & Server Decoupling

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/main.go`
- Modify: `orbit/orbit-cli/cmd/orbit-server/main.go`
- Modify: `orbit/orbit-cli/cmd/orbit/admin.go`
- Test: `orbit/orbit-cli/cmd/orbit/main_test.go`
- Test: `orbit/orbit-cli/cmd/orbit-server/main_test.go`

**Interfaces:**
- Consumes: Cobra `PersistentFlags()`.
- Produces: Global `--config` support across all `orbit` subcommands, clean server-side SMTP resolution.

- [ ] **Step 1: Write failing test for global root `--config` flag**

```go
func TestGlobalConfigFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "custom-orbit.yaml")
	_ = os.WriteFile(cfgPath, []byte("version: 2\nserver:\n  url: https://root-flag.orbit.space\n"), 0600)

	cmd := newRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--config", cfgPath, "config", "get", "server.url", "--raw"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("global flag execution failed: %v", err)
	}
	if buf.String() != "https://root-flag.orbit.space" {
		t.Errorf("expected https://root-flag.orbit.space, got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -v ./cmd/orbit -run "TestGlobalConfigFlag"`
Expected: FAIL.

- [ ] **Step 3: Update `cmd/orbit/main.go`, `cmd/orbit-server/main.go`, and `cmd/orbit/admin.go`**

1. In `cmd/orbit/main.go`:
   - Add `cmd.PersistentFlags().StringVar(&configFlag, "config", "", "Custom path to Orbit CLI configuration file")`.
2. In `cmd/orbit-server/main.go`:
   - Replace direct `cfg.SMTP` reliance with server-side resolution from CLI flags (`--smtp-host`, `--smtp-port`, `--smtp-user`, `--smtp-pass`, `--smtp-from`) and server environment variables (`ORBIT_SMTP_HOST`, `ORBIT_SMTP_PORT`, `ORBIT_SMTP_USER`, `ORBIT_SMTP_PASS`, `ORBIT_SMTP_FROM`).
3. In `cmd/orbit/admin.go`:
   - Update `config.Resolve` call options to match the new `Config` and `ResolveOptions` struct cleanly.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./cmd/orbit -run "TestGlobalConfigFlag"`
Expected: PASS.

---

### Task 7: Full Test Suite Verification & Doc Generation

**Files:**
- Run all unit and integration tests across `orbit-cli`.
- Regenerate documentation via `orbit doc`.

- [ ] **Step 1: Run full test suite across the repo**

Run: `go test -v -race ./...`
Expected: PASS with 0 failures across all packages (`pkg/config`, `pkg/doctor`, `pkg/assets`, `pkg/onboard`, `pkg/invite`, `cmd/orbit`, `cmd/orbit-server`).

- [ ] **Step 2: Regenerate CLI documentation**

Run: `go run ./cmd/orbit doc -f markdown -o docs/cli && go run ./cmd/orbit doc -f man -o docs/cli/man`
Expected: Clean generation of markdown docs and man pages with new subcommands (`orbit config list`, `orbit config unset`).

- [ ] **Step 3: Verify binary build**

Run: `go build -o bin/orbit ./cmd/orbit && go build -o bin/orbit-server ./cmd/orbit-server`
Expected: Clean compilation with 0 warnings or errors.
