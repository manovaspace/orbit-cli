package domain

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrKeyNotFound = errors.New("configuration key not found")

const (
	DefaultServerURL   = "https://orbit.manova.space"
	DefaultStaffURL    = "https://staff.internal.manova.space"
	DefaultAssetBucket = "orbit-assets"
	DefaultScope       = "all"
	DefaultExpiryDays  = 7
	DefaultUIOutput    = "table"
)

// DefaultConfigTemplate is written when `config set` creates a new file.
const DefaultConfigTemplate = `# Orbit CLI Configuration
version: 2

# Core edge server connection
server:
  url: https://orbit.manova.space
  timeout: 15s

# Staff control-plane connection (ADR-024)
staff:
  url: https://staff.internal.manova.space

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

// DefaultConfig returns the canonical configuration.
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

// IsSecretKey reports whether key looks like it carries a credential.
func IsSecretKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "pass") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "key") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "auth")
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

func FlattenCustomMap(prefix string, m map[string]interface{}, entries *[]ConfigEntry) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fullKey := prefix + "." + k
		v := m[k]
		if subMap, ok := v.(map[string]interface{}); ok {
			FlattenCustomMap(fullKey, subMap, entries)
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
	parts := NormalizePath(path)
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
	parts := NormalizePath(path)
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
	parts := NormalizePath(path)
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
		FlattenCustomMap("custom", c.Custom, &entries)
	}

	return entries
}

func (c *Config) Masked() *Config {
	cpy := *c
	return &cpy
}

// NormalizePath splits a dotted key and resolves the documented aliases.
func NormalizePath(path string) []string {
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
