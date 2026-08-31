# Orbit CLI Configuration Architecture & Extensible Namespaces

**Document Version:** 2.0.0  
**Date:** 2026-08-31  
**Status:** Approved — Principal Review Complete  
**Author:** Orbit Platform Team  
**Scope:** `orbit/orbit-cli` (`cmd/orbit/config.go`, `pkg/config/`)

---

## 1. Executive Summary & Core Philosophy

The Orbit developer platform requires a fast, predictable, resilient, and extensible configuration system for developer workstations, local orchestration, and CI pipelines.

### Core Architecture Pillars
1. **Deterministic 5-Tier Precedence**: Explicit Flags > Environment Variables > Workspace Config (`<workspaceRoot>/.orbit/config.yaml`) > Global User Config (`$XDG_CONFIG_HOME/orbit/config.yaml`) > Embedded Defaults.
2. **Strict Secret Isolation**: Zero plaintext credentials on disk in `config.yaml`. SMTP credentials are completely decoupled to server runtime. Workstation secrets reside in dedicated POSIX `0600` vaults (`owner.json`) or environment variables.
3. **Extensible Dot-Notation Engine with AST Preservation**: Strongly typed core domains (`server`, `staff`, `assets`, `defaults`, `ui`) + dynamic extensible map (`custom.*`) using `yaml.Node` tree editing to preserve human comments and YAML layout.
4. **Workspace vs. Global Context Awareness**: Seamless support for per-workspace overrides (e.g. project-specific asset buckets or dev server URLs) without dirtying the global user machine config.
5. **Universal Root Flag Integration**: `--config` is exposed globally across all Orbit commands (`orbit assets`, `orbit doctor`, `orbit sync`, `orbit staff`), not isolated to `orbit config`.
6. **Transparent Source Introspection**: `orbit config list` and `orbit config show` report effective values alongside their active source layer (`default`, `user-config`, `workspace-config`, `env`, or `flag`).

---

## 2. Configuration Hierarchy & Precedence

Configuration values are resolved dynamically in strict descending precedence:

```
┌────────────────────────────────────────────────────────────────────────┐
│ 1. Explicit CLI Flags (e.g., --server, --config)                       │
├────────────────────────────────────────────────────────────────────────┤
│ 2. Environment Variables (ORBIT_SERVER, ORBIT_STAFF_URL, ORBIT_*)      │
├────────────────────────────────────────────────────────────────────────┤
│ 3. Workspace-Local Config (<workspaceRoot>/.orbit/config.yaml)         │
├────────────────────────────────────────────────────────────────────────┤
│ 4. Global User Config ($XDG_CONFIG_HOME/orbit/config.yaml)             │
├────────────────────────────────────────────────────────────────────────┤
│ 5. Canonical Built-in Defaults in Go                                   │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Data Model & Schema (`version: 2`)

### Canonical `config.yaml` Layout

```yaml
version: 2

# Core edge server connection
server:
  url: "https://orbit.manova.space"
  timeout: "15s"

# Staff control-plane connection (ADR-024)
staff:
  url: "https://staff.dev.manova.space"

# Cloudflare R2 Media & Assets (ADR-022)
assets:
  bucket: "orbit-assets"
  endpoint: ""
  auto_pull: true

# Workspace defaults
defaults:
  scope: "all"
  expiry_days: 7

# Terminal formatting & output
ui:
  color: true
  output: "table" # "table" | "json" | "yaml"

# Arbitrary extensions & developer flags
custom:
  telemetry_opt_in: false
  preview_port_override: 10050
```

### Go Data Types (`pkg/config/types.go`)

```go
package config

import "time"

// Source identifies where a configuration parameter was resolved from.
type Source string

const (
	SourceDefault   Source = "default"
	SourceUserFile  Source = "user-config"
	SourceWorkFile  Source = "workspace-config"
	SourceEnv       Source = "env"
	SourceFlag      Source = "flag"
)

type ServerConfig struct {
	URL     string        `yaml:"url" json:"url"`
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
}

type StaffConfig struct {
	URL string `yaml:"url" json:"url"`
}

type AssetsConfig struct {
	Bucket   string `yaml:"bucket" json:"bucket"`
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	AutoPull bool   `yaml:"auto_pull" json:"auto_pull"`
}

type DefaultsConfig struct {
	Scope      string `yaml:"scope" json:"scope"`
	ExpiryDays int    `yaml:"expiry_days" json:"expiry_days"`
}

type UIConfig struct {
	Color  bool   `yaml:"color" json:"color"`
	Output string `yaml:"output" json:"output"` // "table", "json", "yaml"
}

type Config struct {
	Version  int                    `yaml:"version" json:"version"`
	Server   ServerConfig           `yaml:"server" json:"server"`
	Staff    StaffConfig            `yaml:"staff" json:"staff"`
	Assets   AssetsConfig           `yaml:"assets" json:"assets"`
	Defaults DefaultsConfig         `yaml:"defaults" json:"defaults"`
	UI       UIConfig               `yaml:"ui" json:"ui"`
	Custom   map[string]interface{} `yaml:"custom,omitempty" json:"custom,omitempty"`
}

// ConfigEntry wraps an active setting with its origin metadata.
type ConfigEntry struct {
	Key      string `json:"key" yaml:"key"`
	Value    string `json:"value" yaml:"value"`
	Type     string `json:"type" yaml:"type"`
	Source   Source `json:"source" yaml:"source"`
	SourceRef string `json:"source_ref,omitempty" yaml:"source_ref,omitempty"` // e.g. "$ORBIT_SERVER" or filepath
}
```

---

## 4. Extensible Dot-Notation Engine & AST Preservation

### A. Comment-Preserving AST Mutator (`yaml.Node`)
When `orbit config set` modifies `config.yaml`, it reads the file as a `yaml.Node` document AST, locates or creates the key node, updates the scalar value, and writes it back atomically. This **guarantees that human comments and file formatting remain intact**.

### B. Smart Type Inference for Dynamic Keys (`custom.*`)
* `true` / `false` $\rightarrow$ `bool`
* `123`, `-45` $\rightarrow$ `int`
* `15s`, `2m`, `500ms` $\rightarrow$ `time.Duration`
* `["a", "b"]` $\rightarrow$ `[]string`
* All other values $\rightarrow$ `string`

### C. Public Interface (`pkg/config`)

```go
// LoadHierarchy resolves user-global, workspace-local, env, and flag settings.
func Resolve(opts ResolveOptions) (*ResolvedConfig, error)

// Get retrieves a string representation of any dot-path key.
func (c *Config) Get(path string) (string, error)

// Set validates, type-casts, and updates the YAML node tree.
func SetInFile(filePath, path, value string) error

// Unset removes a key from the YAML node tree.
func UnsetInFile(filePath, path string) error

// ListEntries returns a flattened list of all settings with Source metadata.
func (r *ResolvedConfig) ListEntries() []ConfigEntry

// Validate checks strict schema constraints on typed domains.
func (c *Config) Validate() error
```

---

## 5. Security & Secret Decoupling

1. **Workstation vs. Daemon Boundary**:
   * `SMTP` credentials are removed from workstation `Config`.
   * Server daemons (`orbit-server`) load SMTP options directly from server CLI flags (`--smtp-host`, `--smtp-pass`), environment variables (`ORBIT_SMTP_*`), or a dedicated server manifest.
2. **Workstation Secret Stores**:
   * Master platform ownership HMAC keys reside in `~/.config/orbit/owner.json` (mode `0600`).
   * Cloudflare R2 credentials reside in `~/.config/orbit/r2.json` or `$R2_SECRET_ACCESS_KEY`.
3. **Secret Interception Guardrail**:
   * If `orbit config set` is invoked on a key containing sensitive keywords (`pass`, `secret`, `key`, `token`, `auth`), the CLI issues an immediate warning:
   > ⚠️ **Security Warning**: `custom.my_token` appears to be a secret. Plaintext YAML files should not store credentials. Consider using environment variables or `orbit admin` vaults.

---

## 6. CLI Command Specifications

### 1. `orbit config show`
Displays active configuration in YAML, JSON, or table format with source tags.
```bash
orbit config show
orbit config show --format json
```

### 2. `orbit config get <key> [--raw]`
Retrieves a specific value. `--raw` strips trailing newlines and ANSI styles for scripts.
```bash
orbit config get server.url
# Output: https://orbit.manova.space

orbit config get assets.auto_pull --raw
# Output: true
```

### 3. `orbit config set <key> <value> [--local]`
Updates configuration. Passing `--local` writes to `<workspaceRoot>/.orbit/config.yaml` instead of `~/.config/orbit/config.yaml`.
```bash
orbit config set assets.bucket orbit-assets-prod
orbit config set defaults.expiry_days 14
orbit config set custom.dev_mode true
orbit config set assets.bucket custom-client-bucket --local
```

### 4. `orbit config unset <key> [--local]`
Removes a custom key or resets a core setting to default.
```bash
orbit config unset custom.dev_mode
```

### 5. `orbit config list`
Prints an interactive or tabular view of all active settings, their types, and active sources:
```
KEY                  VALUE                       TYPE       SOURCE
server.url           https://orbit.manova.space  string     env ($ORBIT_SERVER)
server.timeout       15s                         duration   default
staff.url            https://staff.dev.manova... string     user-config (~/.config/orbit/config.yaml)
assets.bucket        orbit-assets                string     workspace-config (.orbit/config.yaml)
assets.auto_pull     true                        bool       default
defaults.scope       all                         string     default
defaults.expiry_days 7                           int        default
ui.color             true                        bool       default
ui.output            table                       string     default
```

### 6. `orbit config init [--local] [--force]`
Initializes a clean `config.yaml` file with mode `0600`.

### 7. `orbit config path [--local]`
Prints the active config file path (`--local` prints the workspace-level path if inside a workspace).

---

## 7. Migration & Backward Compatibility

1. **Auto-Migration on Startup**:
   * If `~/.config/orbit/` does not exist and `~/.config/manova/` exists:
     * Copies `~/.config/manova/config.yaml` $\rightarrow$ `~/.config/orbit/config.yaml`.
     * Copies `~/.config/manova/invites.json` $\rightarrow$ `~/.config/orbit/invites.json`.
     * Emits a subtle log: `ℹ Migrated legacy configuration from ~/.config/manova/ to ~/.config/orbit/`.
2. **Version 1 $\rightarrow$ Version 2 Upgrade**:
   * When loading a `version: 1` config with an `smtp:` block, the engine silently drops the workstation SMTP credentials and persists `version: 2`.

---

## 8. Verification & Test Suite

1. **Unit Tests (`pkg/config/config_test.go`)**:
   * Precedence test matrix: Flag > Env > Workspace File > Global File > Default.
   * `yaml.Node` AST preservation: Verify comments are preserved after `SetInFile()` and `UnsetInFile()`.
   * Type inference and casting tests for `custom.*` keys.
   * Validation tests for invalid URLs, negative expiry days, invalid duration syntax.
   * Atomic write and POSIX `0600` permission tests.
2. **CLI Integration Tests (`cmd/orbit/config_test.go`)**:
   * `orbit config list` source tagging verification.
   * `--local` workspace config override testing.
   * Secret keyword warning trigger tests.
   * Legacy `~/.config/manova` auto-migration tests.
