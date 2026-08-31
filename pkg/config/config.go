package config

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var ErrKeyNotFound = errors.New("configuration key not found")

const (
	DefaultServerURL   = "https://orbit.manova.space"
	DefaultStaffURL    = "https://staff.dev.manova.space"
	DefaultAssetBucket = "orbit-assets"
	DefaultScope       = "all"
	DefaultExpiryDays  = 7
	DefaultUIOutput    = "table"
)

func DefaultConfig() *Config {
	return &Config{
		Version: 2,
		Server: ServerConfig{
			URL:     DefaultServerURL,
			Timeout: 15 * time.Second,
		},
		Staff: StaffConfig{
			URL: DefaultStaffURL,
		},
		Assets: AssetsConfig{
			Bucket:   DefaultAssetBucket,
			Endpoint: "",
			AutoPull: true,
		},
		Defaults: DefaultsConfig{
			Scope:      DefaultScope,
			ExpiryDays: DefaultExpiryDays,
		},
		UI: UIConfig{
			Color:  true,
			Output: DefaultUIOutput,
		},
		Custom: make(map[string]interface{}),
	}
}

func DefaultConfigPath() string {
	if p := strings.TrimSpace(os.Getenv("ORBIT_CONFIG")); p != "" {
		return p
	}
	configDir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "orbit", "config.yaml")
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "orbit", "config.yaml")
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	// Auto-tighten permissions if wider than 0600
	if info.Mode().Perm()&0077 != 0 || info.Mode().Perm() > 0600 {
		_ = os.Chmod(path, 0600)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config yaml: %w", err)
	}
	if cfg.Custom == nil {
		cfg.Custom = make(map[string]interface{})
	}
	return cfg, nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-config-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to atomic rename file to %q: %w", path, err)
	}

	return nil
}

func (c *Config) Save(path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to encode config yaml: %w", err)
	}

	return atomicWriteFile(path, data, 0600)
}

func inferValue(raw string) interface{} {
	trimmed := strings.TrimSpace(raw)
	lower := strings.ToLower(trimmed)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}
	if i, err := strconv.Atoi(trimmed); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f
	}
	return raw
}

func formatValue(val interface{}) (string, string) {
	if val == nil {
		return "", "string"
	}
	switch v := val.(type) {
	case bool:
		return strconv.FormatBool(v), "bool"
	case int:
		return strconv.Itoa(v), "int"
	case int64:
		return strconv.FormatInt(v, 10), "int"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), "float"
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 64), "float"
	case string:
		return v, "string"
	case time.Duration:
		return v.String(), "duration"
	default:
		return fmt.Sprintf("%v", v), fmt.Sprintf("%T", v)
	}
}

func getCustomNested(m map[string]interface{}, parts []string) (interface{}, bool) {
	if len(parts) == 0 || m == nil {
		return nil, false
	}
	curr := m
	for i, part := range parts {
		val, exists := curr[part]
		if !exists {
			return nil, false
		}
		if i == len(parts)-1 {
			return val, true
		}
		nextMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		curr = nextMap
	}
	return nil, false
}

func setCustomNested(m map[string]interface{}, parts []string, val interface{}) {
	if len(parts) == 0 || m == nil {
		return
	}
	curr := m
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		next, ok := curr[part]
		if !ok {
			nextMap := make(map[string]interface{})
			curr[part] = nextMap
			curr = nextMap
		} else {
			nextMap, ok := next.(map[string]interface{})
			if !ok {
				nextMap = make(map[string]interface{})
				curr[part] = nextMap
			}
			curr = nextMap
		}
	}
	curr[parts[len(parts)-1]] = val
}

func unsetCustomNested(m map[string]interface{}, parts []string) {
	if len(parts) == 0 || m == nil {
		return
	}
	if len(parts) == 1 {
		delete(m, parts[0])
		return
	}
	part := parts[0]
	if next, ok := m[part]; ok {
		if nextMap, ok := next.(map[string]interface{}); ok {
			unsetCustomNested(nextMap, parts[1:])
		}
	}
}

func flattenCustomMap(prefix string, m map[string]interface{}, entries *[]ConfigEntry) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fullKey := prefix + "." + k
		v := m[k]
		if subMap, ok := v.(map[string]interface{}); ok {
			flattenCustomMap(fullKey, subMap, entries)
		} else {
			strVal, typeName := formatValue(v)
			*entries = append(*entries, ConfigEntry{
				Key:    fullKey,
				Value:  strVal,
				Type:   typeName,
				Source: SourceUserFile,
			})
		}
	}
}

func validateURL(field, rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	u, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", field, rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid %s %q: scheme must be http or https", field, rawURL)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid %s %q: host must not be empty", field, rawURL)
	}
	return nil
}

func (c *Config) Validate() error {
	if err := validateURL("server.url", c.Server.URL); err != nil {
		return err
	}
	if err := validateURL("staff.url", c.Staff.URL); err != nil {
		return err
	}
	if strings.TrimSpace(c.Assets.Bucket) == "" {
		return errors.New("assets.bucket must not be empty")
	}
	if c.Defaults.ExpiryDays <= 0 {
		return fmt.Errorf("defaults.expiry_days must be positive integer (> 0), got %d", c.Defaults.ExpiryDays)
	}
	out := strings.ToLower(strings.TrimSpace(c.UI.Output))
	if out != "table" && out != "json" && out != "yaml" {
		return fmt.Errorf("ui.output must be one of 'table', 'json', 'yaml', got %q", c.UI.Output)
	}
	return nil
}

func (c *Config) Get(path string) (ConfigEntry, error) {
	parts := normalizePath(path)
	if len(parts) == 0 {
		return ConfigEntry{}, fmt.Errorf("%w: invalid empty path", ErrKeyNotFound)
	}

	domain := strings.ToLower(parts[0])
	switch domain {
	case "version":
		return ConfigEntry{
			Key:    "version",
			Value:  strconv.Itoa(c.Version),
			Type:   "int",
			Source: SourceUserFile,
		}, nil

	case "server":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "url":
				return ConfigEntry{
					Key:    "server.url",
					Value:  c.Server.URL,
					Type:   "string",
					Source: SourceUserFile,
				}, nil
			case "timeout":
				return ConfigEntry{
					Key:    "server.timeout",
					Value:  c.Server.Timeout.String(),
					Type:   "duration",
					Source: SourceUserFile,
				}, nil
			}
		}
		return ConfigEntry{}, fmt.Errorf("%w: unknown server property %q", ErrKeyNotFound, path)

	case "staff":
		if len(parts) == 2 && strings.ToLower(parts[1]) == "url" {
			return ConfigEntry{
				Key:    "staff.url",
				Value:  c.Staff.URL,
				Type:   "string",
				Source: SourceUserFile,
			}, nil
		}
		return ConfigEntry{}, fmt.Errorf("%w: unknown staff property %q", ErrKeyNotFound, path)

	case "assets":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "bucket":
				return ConfigEntry{
					Key:    "assets.bucket",
					Value:  c.Assets.Bucket,
					Type:   "string",
					Source: SourceUserFile,
				}, nil
			case "endpoint":
				return ConfigEntry{
					Key:    "assets.endpoint",
					Value:  c.Assets.Endpoint,
					Type:   "string",
					Source: SourceUserFile,
				}, nil
			case "auto_pull", "autopull":
				return ConfigEntry{
					Key:    "assets.auto_pull",
					Value:  strconv.FormatBool(c.Assets.AutoPull),
					Type:   "bool",
					Source: SourceUserFile,
				}, nil
			}
		}
		return ConfigEntry{}, fmt.Errorf("%w: unknown assets property %q", ErrKeyNotFound, path)

	case "defaults":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "scope":
				return ConfigEntry{
					Key:    "defaults.scope",
					Value:  c.Defaults.Scope,
					Type:   "string",
					Source: SourceUserFile,
				}, nil
			case "expiry_days", "expirydays":
				return ConfigEntry{
					Key:    "defaults.expiry_days",
					Value:  strconv.Itoa(c.Defaults.ExpiryDays),
					Type:   "int",
					Source: SourceUserFile,
				}, nil
			}
		}
		return ConfigEntry{}, fmt.Errorf("%w: unknown defaults property %q", ErrKeyNotFound, path)

	case "ui":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "color":
				return ConfigEntry{
					Key:    "ui.color",
					Value:  strconv.FormatBool(c.UI.Color),
					Type:   "bool",
					Source: SourceUserFile,
				}, nil
			case "output":
				return ConfigEntry{
					Key:    "ui.output",
					Value:  c.UI.Output,
					Type:   "string",
					Source: SourceUserFile,
				}, nil
			}
		}
		return ConfigEntry{}, fmt.Errorf("%w: unknown ui property %q", ErrKeyNotFound, path)

	case "custom":
		if len(parts) == 1 {
			return ConfigEntry{}, fmt.Errorf("%w: cannot get root custom namespace directly", ErrKeyNotFound)
		}
		subparts := parts[1:]
		if c.Custom == nil {
			return ConfigEntry{}, fmt.Errorf("%w: custom key %q not found", ErrKeyNotFound, path)
		}
		val, found := getCustomNested(c.Custom, subparts)
		if !found {
			return ConfigEntry{}, fmt.Errorf("%w: custom key %q not found", ErrKeyNotFound, path)
		}
		strVal, typeName := formatValue(val)
		canonicalKey := "custom." + strings.Join(subparts, ".")
		return ConfigEntry{
			Key:    canonicalKey,
			Value:  strVal,
			Type:   typeName,
			Source: SourceUserFile,
		}, nil

	default:
		return ConfigEntry{}, fmt.Errorf("%w: unknown configuration key %q", ErrKeyNotFound, path)
	}
}

func (c *Config) Set(path, value string) error {
	parts := normalizePath(path)
	if len(parts) == 0 {
		return errors.New("invalid empty path")
	}

	v := strings.TrimSpace(value)
	domain := strings.ToLower(parts[0])

	switch domain {
	case "version":
		ver, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", v, err)
		}
		c.Version = ver
		return nil

	case "server":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "url":
				c.Server.URL = v
				return nil
			case "timeout":
				d, err := time.ParseDuration(v)
				if err != nil {
					return fmt.Errorf("invalid duration %q: %w", v, err)
				}
				c.Server.Timeout = d
				return nil
			}
		}
		return fmt.Errorf("unknown server property %q", path)

	case "staff":
		if len(parts) == 2 && strings.ToLower(parts[1]) == "url" {
			c.Staff.URL = v
			return nil
		}
		return fmt.Errorf("unknown staff property %q", path)

	case "assets":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "bucket":
				c.Assets.Bucket = v
				return nil
			case "endpoint":
				c.Assets.Endpoint = v
				return nil
			case "auto_pull", "autopull":
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid boolean %q: %w", v, err)
				}
				c.Assets.AutoPull = b
				return nil
			}
		}
		return fmt.Errorf("unknown assets property %q", path)

	case "defaults":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "scope":
				c.Defaults.Scope = v
				return nil
			case "expiry_days", "expirydays":
				days, err := strconv.Atoi(v)
				if err != nil || days <= 0 {
					return fmt.Errorf("invalid expiry_days %q: must be positive integer", v)
				}
				c.Defaults.ExpiryDays = days
				return nil
			}
		}
		return fmt.Errorf("unknown defaults property %q", path)

	case "ui":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "color":
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("invalid boolean %q: %w", v, err)
				}
				c.UI.Color = b
				return nil
			case "output":
				out := strings.ToLower(v)
				if out != "table" && out != "json" && out != "yaml" {
					return fmt.Errorf("invalid ui.output %q: must be one of 'table', 'json', 'yaml'", v)
				}
				c.UI.Output = out
				return nil
			}
		}
		return fmt.Errorf("unknown ui property %q", path)

	case "custom":
		if len(parts) == 1 {
			return errors.New("cannot set root custom namespace directly; specify a key like custom.foo")
		}
		if c.Custom == nil {
			c.Custom = make(map[string]interface{})
		}
		subparts := parts[1:]
		inferred := inferValue(v)
		setCustomNested(c.Custom, subparts, inferred)
		return nil

	default:
		return fmt.Errorf("unknown configuration key %q", path)
	}
}

func (c *Config) Unset(path string) error {
	parts := normalizePath(path)
	if len(parts) == 0 {
		return errors.New("invalid empty path")
	}

	domain := strings.ToLower(parts[0])
	defaults := DefaultConfig()

	switch domain {
	case "version":
		c.Version = defaults.Version
		return nil

	case "server":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "url":
				c.Server.URL = defaults.Server.URL
				return nil
			case "timeout":
				c.Server.Timeout = defaults.Server.Timeout
				return nil
			}
		}
		return fmt.Errorf("unknown server property %q", path)

	case "staff":
		if len(parts) == 2 && strings.ToLower(parts[1]) == "url" {
			c.Staff.URL = defaults.Staff.URL
			return nil
		}
		return fmt.Errorf("unknown staff property %q", path)

	case "assets":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "bucket":
				c.Assets.Bucket = defaults.Assets.Bucket
				return nil
			case "endpoint":
				c.Assets.Endpoint = defaults.Assets.Endpoint
				return nil
			case "auto_pull", "autopull":
				c.Assets.AutoPull = defaults.Assets.AutoPull
				return nil
			}
		}
		return fmt.Errorf("unknown assets property %q", path)

	case "defaults":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "scope":
				c.Defaults.Scope = defaults.Defaults.Scope
				return nil
			case "expiry_days", "expirydays":
				c.Defaults.ExpiryDays = defaults.Defaults.ExpiryDays
				return nil
			}
		}
		return fmt.Errorf("unknown defaults property %q", path)

	case "ui":
		if len(parts) == 2 {
			switch strings.ToLower(parts[1]) {
			case "color":
				c.UI.Color = defaults.UI.Color
				return nil
			case "output":
				c.UI.Output = defaults.UI.Output
				return nil
			}
		}
		return fmt.Errorf("unknown ui property %q", path)

	case "custom":
		if len(parts) == 1 {
			c.Custom = make(map[string]interface{})
			return nil
		}
		if c.Custom != nil {
			unsetCustomNested(c.Custom, parts[1:])
		}
		return nil

	default:
		return fmt.Errorf("unknown configuration key %q", path)
	}
}

func (c *Config) Entries() []ConfigEntry {
	entries := []ConfigEntry{
		{Key: "server.url", Value: c.Server.URL, Type: "string", Source: SourceUserFile},
		{Key: "server.timeout", Value: c.Server.Timeout.String(), Type: "duration", Source: SourceUserFile},
		{Key: "staff.url", Value: c.Staff.URL, Type: "string", Source: SourceUserFile},
		{Key: "assets.bucket", Value: c.Assets.Bucket, Type: "string", Source: SourceUserFile},
		{Key: "assets.endpoint", Value: c.Assets.Endpoint, Type: "string", Source: SourceUserFile},
		{Key: "assets.auto_pull", Value: strconv.FormatBool(c.Assets.AutoPull), Type: "bool", Source: SourceUserFile},
		{Key: "defaults.scope", Value: c.Defaults.Scope, Type: "string", Source: SourceUserFile},
		{Key: "defaults.expiry_days", Value: strconv.Itoa(c.Defaults.ExpiryDays), Type: "int", Source: SourceUserFile},
		{Key: "ui.color", Value: strconv.FormatBool(c.UI.Color), Type: "bool", Source: SourceUserFile},
		{Key: "ui.output", Value: c.UI.Output, Type: "string", Source: SourceUserFile},
	}

	if c.Custom != nil {
		flattenCustomMap("custom", c.Custom, &entries)
	}

	return entries
}

func (c *Config) Masked() *Config {
	cpy := *c
	return &cpy
}

func Resolve(opts ResolveOptions) (*ResolvedConfig, error) {
	cfg := DefaultConfig()

	// Tier 1: Canonical Defaults
	entriesMap := map[string]*ConfigEntry{
		"server.url":           {Key: "server.url", Value: cfg.Server.URL, Type: "string", Source: SourceDefault, SourceRef: "default"},
		"server.timeout":       {Key: "server.timeout", Value: cfg.Server.Timeout.String(), Type: "duration", Source: SourceDefault, SourceRef: "default"},
		"staff.url":            {Key: "staff.url", Value: cfg.Staff.URL, Type: "string", Source: SourceDefault, SourceRef: "default"},
		"assets.bucket":        {Key: "assets.bucket", Value: cfg.Assets.Bucket, Type: "string", Source: SourceDefault, SourceRef: "default"},
		"assets.endpoint":      {Key: "assets.endpoint", Value: cfg.Assets.Endpoint, Type: "string", Source: SourceDefault, SourceRef: "default"},
		"assets.auto_pull":     {Key: "assets.auto_pull", Value: strconv.FormatBool(cfg.Assets.AutoPull), Type: "bool", Source: SourceDefault, SourceRef: "default"},
		"defaults.scope":       {Key: "defaults.scope", Value: cfg.Defaults.Scope, Type: "string", Source: SourceDefault, SourceRef: "default"},
		"defaults.expiry_days": {Key: "defaults.expiry_days", Value: strconv.Itoa(cfg.Defaults.ExpiryDays), Type: "int", Source: SourceDefault, SourceRef: "default"},
		"ui.color":             {Key: "ui.color", Value: strconv.FormatBool(cfg.UI.Color), Type: "bool", Source: SourceDefault, SourceRef: "default"},
		"ui.output":            {Key: "ui.output", Value: cfg.UI.Output, Type: "string", Source: SourceDefault, SourceRef: "default"},
	}
	var customEntries []ConfigEntry

	// Tier 2: User Config File
	path := opts.ConfigPath
	if path == "" {
		path = DefaultConfigPath()
	}

	if data, err := os.ReadFile(path); err == nil {
		if info, err := os.Stat(path); err == nil {
			if info.Mode().Perm()&0077 != 0 || info.Mode().Perm() > 0600 {
				_ = os.Chmod(path, 0600)
			}
		}

		var rawMap map[string]interface{}
		_ = yaml.Unmarshal(data, &rawMap)

		var fileCfg Config
		fileCfg.Version = cfg.Version
		fileCfg.Server = cfg.Server
		fileCfg.Staff = cfg.Staff
		fileCfg.Assets = cfg.Assets
		fileCfg.Defaults = cfg.Defaults
		fileCfg.UI = cfg.UI
		fileCfg.Custom = make(map[string]interface{})

		if err := yaml.Unmarshal(data, &fileCfg); err == nil {
			if sMap, ok := rawMap["server"].(map[string]interface{}); ok {
				if _, ok := sMap["url"]; ok {
					cfg.Server.URL = fileCfg.Server.URL
					entriesMap["server.url"] = &ConfigEntry{Key: "server.url", Value: cfg.Server.URL, Type: "string", Source: SourceUserFile, SourceRef: path}
				}
				if _, ok := sMap["timeout"]; ok {
					cfg.Server.Timeout = fileCfg.Server.Timeout
					entriesMap["server.timeout"] = &ConfigEntry{Key: "server.timeout", Value: cfg.Server.Timeout.String(), Type: "duration", Source: SourceUserFile, SourceRef: path}
				}
			}
			if stMap, ok := rawMap["staff"].(map[string]interface{}); ok {
				if _, ok := stMap["url"]; ok {
					cfg.Staff.URL = fileCfg.Staff.URL
					entriesMap["staff.url"] = &ConfigEntry{Key: "staff.url", Value: cfg.Staff.URL, Type: "string", Source: SourceUserFile, SourceRef: path}
				}
			}
			if aMap, ok := rawMap["assets"].(map[string]interface{}); ok {
				if _, ok := aMap["bucket"]; ok {
					cfg.Assets.Bucket = fileCfg.Assets.Bucket
					entriesMap["assets.bucket"] = &ConfigEntry{Key: "assets.bucket", Value: cfg.Assets.Bucket, Type: "string", Source: SourceUserFile, SourceRef: path}
				}
				if _, ok := aMap["endpoint"]; ok {
					cfg.Assets.Endpoint = fileCfg.Assets.Endpoint
					entriesMap["assets.endpoint"] = &ConfigEntry{Key: "assets.endpoint", Value: cfg.Assets.Endpoint, Type: "string", Source: SourceUserFile, SourceRef: path}
				}
				if _, ok := aMap["auto_pull"]; ok {
					cfg.Assets.AutoPull = fileCfg.Assets.AutoPull
					entriesMap["assets.auto_pull"] = &ConfigEntry{Key: "assets.auto_pull", Value: strconv.FormatBool(cfg.Assets.AutoPull), Type: "bool", Source: SourceUserFile, SourceRef: path}
				} else if _, ok := aMap["autopull"]; ok {
					cfg.Assets.AutoPull = fileCfg.Assets.AutoPull
					entriesMap["assets.auto_pull"] = &ConfigEntry{Key: "assets.auto_pull", Value: strconv.FormatBool(cfg.Assets.AutoPull), Type: "bool", Source: SourceUserFile, SourceRef: path}
				}
			}
			if dMap, ok := rawMap["defaults"].(map[string]interface{}); ok {
				if _, ok := dMap["scope"]; ok {
					cfg.Defaults.Scope = fileCfg.Defaults.Scope
					entriesMap["defaults.scope"] = &ConfigEntry{Key: "defaults.scope", Value: cfg.Defaults.Scope, Type: "string", Source: SourceUserFile, SourceRef: path}
				}
				if _, ok := dMap["expiry_days"]; ok {
					cfg.Defaults.ExpiryDays = fileCfg.Defaults.ExpiryDays
					entriesMap["defaults.expiry_days"] = &ConfigEntry{Key: "defaults.expiry_days", Value: strconv.Itoa(cfg.Defaults.ExpiryDays), Type: "int", Source: SourceUserFile, SourceRef: path}
				} else if _, ok := dMap["expirydays"]; ok {
					cfg.Defaults.ExpiryDays = fileCfg.Defaults.ExpiryDays
					entriesMap["defaults.expiry_days"] = &ConfigEntry{Key: "defaults.expiry_days", Value: strconv.Itoa(cfg.Defaults.ExpiryDays), Type: "int", Source: SourceUserFile, SourceRef: path}
				}
			}
			if uMap, ok := rawMap["ui"].(map[string]interface{}); ok {
				if _, ok := uMap["color"]; ok {
					cfg.UI.Color = fileCfg.UI.Color
					entriesMap["ui.color"] = &ConfigEntry{Key: "ui.color", Value: strconv.FormatBool(cfg.UI.Color), Type: "bool", Source: SourceUserFile, SourceRef: path}
				}
				if _, ok := uMap["output"]; ok {
					cfg.UI.Output = fileCfg.UI.Output
					entriesMap["ui.output"] = &ConfigEntry{Key: "ui.output", Value: cfg.UI.Output, Type: "string", Source: SourceUserFile, SourceRef: path}
				}
			}
			if fileCfg.Custom != nil {
				cfg.Custom = fileCfg.Custom
				flattenCustomMap("custom", cfg.Custom, &customEntries)
				for i := range customEntries {
					customEntries[i].Source = SourceUserFile
					customEntries[i].SourceRef = path
				}
			}
		}
	}

	// Tier 3: Environment Variables
	if val := strings.TrimSpace(os.Getenv("ORBIT_SERVER")); val != "" {
		cfg.Server.URL = val
		entriesMap["server.url"] = &ConfigEntry{Key: "server.url", Value: val, Type: "string", Source: SourceEnv, SourceRef: "$ORBIT_SERVER"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_SERVER_TIMEOUT")); val != "" {
		d, err := time.ParseDuration(val)
		if err != nil {
			return nil, fmt.Errorf("invalid ORBIT_SERVER_TIMEOUT %q: %w", val, err)
		}
		cfg.Server.Timeout = d
		entriesMap["server.timeout"] = &ConfigEntry{Key: "server.timeout", Value: d.String(), Type: "duration", Source: SourceEnv, SourceRef: "$ORBIT_SERVER_TIMEOUT"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_STAFF_URL")); val != "" {
		cfg.Staff.URL = val
		entriesMap["staff.url"] = &ConfigEntry{Key: "staff.url", Value: val, Type: "string", Source: SourceEnv, SourceRef: "$ORBIT_STAFF_URL"}
	} else if val := strings.TrimSpace(os.Getenv("ORBIT_STAFF")); val != "" {
		cfg.Staff.URL = val
		entriesMap["staff.url"] = &ConfigEntry{Key: "staff.url", Value: val, Type: "string", Source: SourceEnv, SourceRef: "$ORBIT_STAFF"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_ASSETS_BUCKET")); val != "" {
		cfg.Assets.Bucket = val
		entriesMap["assets.bucket"] = &ConfigEntry{Key: "assets.bucket", Value: val, Type: "string", Source: SourceEnv, SourceRef: "$ORBIT_ASSETS_BUCKET"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_ASSETS_ENDPOINT")); val != "" {
		cfg.Assets.Endpoint = val
		entriesMap["assets.endpoint"] = &ConfigEntry{Key: "assets.endpoint", Value: val, Type: "string", Source: SourceEnv, SourceRef: "$ORBIT_ASSETS_ENDPOINT"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_ASSETS_AUTO_PULL")); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid ORBIT_ASSETS_AUTO_PULL %q: %w", val, err)
		}
		cfg.Assets.AutoPull = b
		entriesMap["assets.auto_pull"] = &ConfigEntry{Key: "assets.auto_pull", Value: strconv.FormatBool(b), Type: "bool", Source: SourceEnv, SourceRef: "$ORBIT_ASSETS_AUTO_PULL"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_DEFAULTS_SCOPE")); val != "" {
		cfg.Defaults.Scope = val
		entriesMap["defaults.scope"] = &ConfigEntry{Key: "defaults.scope", Value: val, Type: "string", Source: SourceEnv, SourceRef: "$ORBIT_DEFAULTS_SCOPE"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_DEFAULTS_EXPIRY_DAYS")); val != "" {
		days, err := strconv.Atoi(val)
		if err != nil || days <= 0 {
			return nil, fmt.Errorf("invalid ORBIT_DEFAULTS_EXPIRY_DAYS %q: must be positive integer (> 0)", val)
		}
		cfg.Defaults.ExpiryDays = days
		entriesMap["defaults.expiry_days"] = &ConfigEntry{Key: "defaults.expiry_days", Value: strconv.Itoa(days), Type: "int", Source: SourceEnv, SourceRef: "$ORBIT_DEFAULTS_EXPIRY_DAYS"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_UI_COLOR")); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid ORBIT_UI_COLOR %q: %w", val, err)
		}
		cfg.UI.Color = b
		entriesMap["ui.color"] = &ConfigEntry{Key: "ui.color", Value: strconv.FormatBool(b), Type: "bool", Source: SourceEnv, SourceRef: "$ORBIT_UI_COLOR"}
	}
	if val := strings.TrimSpace(os.Getenv("ORBIT_UI_OUTPUT")); val != "" {
		cfg.UI.Output = val
		entriesMap["ui.output"] = &ConfigEntry{Key: "ui.output", Value: val, Type: "string", Source: SourceEnv, SourceRef: "$ORBIT_UI_OUTPUT"}
	}

	// Tier 4: Explicit CLI Flags
	if opts.ServerFlag != "" {
		cfg.Server.URL = opts.ServerFlag
		entriesMap["server.url"] = &ConfigEntry{Key: "server.url", Value: opts.ServerFlag, Type: "string", Source: SourceFlag, SourceRef: "--server"}
	}
	if opts.StaffURLFlag != "" {
		cfg.Staff.URL = opts.StaffURLFlag
		entriesMap["staff.url"] = &ConfigEntry{Key: "staff.url", Value: opts.StaffURLFlag, Type: "string", Source: SourceFlag, SourceRef: "--staff-url"}
	}
	if opts.AssetsBucketFlag != "" {
		cfg.Assets.Bucket = opts.AssetsBucketFlag
		entriesMap["assets.bucket"] = &ConfigEntry{Key: "assets.bucket", Value: opts.AssetsBucketFlag, Type: "string", Source: SourceFlag, SourceRef: "--bucket"}
	}
	if opts.ScopeFlag != "" {
		cfg.Defaults.Scope = opts.ScopeFlag
		entriesMap["defaults.scope"] = &ConfigEntry{Key: "defaults.scope", Value: opts.ScopeFlag, Type: "string", Source: SourceFlag, SourceRef: "--scope"}
	}

	// Step 5: Validation
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Step 6: Return
	coreKeys := []string{
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

	entries := make([]ConfigEntry, 0, len(coreKeys)+len(customEntries))
	for _, k := range coreKeys {
		if e, ok := entriesMap[k]; ok && e != nil {
			entries = append(entries, *e)
		}
	}
	entries = append(entries, customEntries...)

	return &ResolvedConfig{
		Config:  cfg,
		Entries: entries,
	}, nil
}

const defaultConfigTemplate = `# Orbit CLI Configuration
version: 2

# Core edge server connection
server:
  url: https://orbit.manova.space
  timeout: 15s

# Staff control-plane connection (ADR-024)
staff:
  url: https://staff.dev.manova.space

# Cloudflare R2 Media & Assets (ADR-022)
assets:
  bucket: orbit-assets
  endpoint: ""
  auto_pull: true

# Workspace defaults
defaults:
  scope: all
  expiry_days: 7

# Terminal formatting & output
ui:
  color: true
  output: table

# Arbitrary extensions & developer flags
custom: {}
`

func normalizePath(path string) []string {
	var cleanParts []string
	for _, p := range strings.Split(path, ".") {
		p = strings.TrimSpace(p)
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}
	if len(cleanParts) == 1 {
		switch strings.ToLower(cleanParts[0]) {
		case "server":
			return []string{"server", "url"}
		case "staff":
			return []string{"staff", "url"}
		}
	}
	if len(cleanParts) == 2 {
		if strings.ToLower(cleanParts[0]) == "defaults" && strings.ToLower(cleanParts[1]) == "expirydays" {
			return []string{"defaults", "expiry_days"}
		}
		if strings.ToLower(cleanParts[0]) == "assets" && strings.ToLower(cleanParts[1]) == "autopull" {
			return []string{"assets", "auto_pull"}
		}
	}
	return cleanParts
}

func unwrapMappingNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			mapNode := &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
			node.Content = append(node.Content, mapNode)
			return mapNode
		}
		if node.Content[0].Kind != yaml.MappingNode {
			node.Content[0] = &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
		}
		return node.Content[0]
	}
	if node.Kind == yaml.MappingNode {
		return node
	}
	return nil
}

func setScalarValue(node *yaml.Node, value string) {
	node.Kind = yaml.ScalarNode
	node.Value = value
	node.Content = nil

	lower := strings.ToLower(value)
	if lower == "true" || lower == "false" {
		node.Tag = "!!bool"
		node.Value = lower
		node.Style = 0
	} else if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		node.Tag = "!!int"
		node.Style = 0
	} else {
		node.Tag = "!!str"
	}
}

func modifyNode(node *yaml.Node, parts []string, value string) error {
	if node == nil {
		return errors.New("node is nil")
	}
	if len(parts) == 0 {
		return errors.New("path cannot be empty")
	}

	current := unwrapMappingNode(node)
	if current == nil {
		return errors.New("root node must be a document or mapping node")
	}

	for i, part := range parts {
		isLeaf := (i == len(parts)-1)

		var targetValNode *yaml.Node
		for j := 0; j < len(current.Content); j += 2 {
			keyNode := current.Content[j]
			if keyNode.Value == part {
				targetValNode = current.Content[j+1]
				break
			}
		}

		if isLeaf {
			if targetValNode != nil {
				setScalarValue(targetValNode, value)
			} else {
				keyNode := &yaml.Node{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: part,
				}
				valNode := &yaml.Node{}
				setScalarValue(valNode, value)
				current.Content = append(current.Content, keyNode, valNode)
			}
			return nil
		}

		// Intermediate node
		if targetValNode == nil {
			keyNode := &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: part,
			}
			valNode := &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
			current.Content = append(current.Content, keyNode, valNode)
			current = valNode
		} else {
			if targetValNode.Kind != yaml.MappingNode {
				targetValNode.Kind = yaml.MappingNode
				targetValNode.Tag = "!!map"
				targetValNode.Value = ""
				targetValNode.Content = nil
			}
			current = targetValNode
		}
	}

	return nil
}

func deleteNode(node *yaml.Node, parts []string) error {
	if node == nil {
		return errors.New("node is nil")
	}
	if len(parts) == 0 {
		return errors.New("path cannot be empty")
	}

	current := unwrapMappingNode(node)
	if current == nil {
		return errors.New("root node must be a document or mapping node")
	}

	deleteNodeHelper(current, parts)
	return nil
}

func deleteNodeHelper(parent *yaml.Node, parts []string) {
	if parent == nil || parent.Kind != yaml.MappingNode || len(parts) == 0 {
		return
	}

	targetKey := parts[0]
	for i := 0; i < len(parent.Content); i += 2 {
		keyNode := parent.Content[i]
		if keyNode.Value == targetKey {
			valNode := parent.Content[i+1]
			if len(parts) == 1 {
				// Leaf reached: remove key and value nodes
				parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
				return
			}
			if valNode.Kind == yaml.MappingNode {
				deleteNodeHelper(valNode, parts[1:])
			}
			return
		}
	}
}

func SetInFile(filePath, path, value string) error {
	if filePath == "" {
		filePath = DefaultConfigPath()
	}

	parts := normalizePath(path)
	if len(parts) == 0 {
		return errors.New("invalid empty path")
	}

	var doc yaml.Node
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			data = []byte(defaultConfigTemplate)
		} else {
			return fmt.Errorf("failed to read config file %q: %w", filePath, err)
		}
	}

	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse yaml in %q: %w", filePath, err)
	}

	if err := modifyNode(&doc, parts, value); err != nil {
		return fmt.Errorf("failed to modify yaml node: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return fmt.Errorf("failed to encode yaml node: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to close yaml encoder: %w", err)
	}

	return atomicWriteFile(filePath, buf.Bytes(), 0600)
}

func UnsetInFile(filePath, path string) error {
	if filePath == "" {
		filePath = DefaultConfigPath()
	}

	parts := normalizePath(path)
	if len(parts) == 0 {
		return errors.New("invalid empty path")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read config file %q: %w", filePath, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse yaml in %q: %w", filePath, err)
	}

	if err := deleteNode(&doc, parts); err != nil {
		return fmt.Errorf("failed to delete yaml node: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return fmt.Errorf("failed to encode yaml node: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to close yaml encoder: %w", err)
	}

	return atomicWriteFile(filePath, buf.Bytes(), 0600)
}
