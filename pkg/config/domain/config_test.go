package domain

import (
	"testing"
	"time"
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
	if cfg.Staff.URL != "https://staff.internal.manova.space" {
		t.Errorf("expected default staff URL https://staff.internal.manova.space, got %s", cfg.Staff.URL)
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
