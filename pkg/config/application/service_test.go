package application

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/config/domain"
)

type ConfigEntry = domain.ConfigEntry

const (
	DefaultServerURL   = domain.DefaultServerURL
	DefaultStaffURL    = domain.DefaultStaffURL
	DefaultAssetBucket = domain.DefaultAssetBucket
	DefaultScope       = domain.DefaultScope
	DefaultExpiryDays  = domain.DefaultExpiryDays
	DefaultUIOutput    = domain.DefaultUIOutput

	SourceDefault  = domain.SourceDefault
	SourceUserFile = domain.SourceUserFile
	SourceEnv      = domain.SourceEnv
	SourceFlag     = domain.SourceFlag
)

// testStore reads real temp files but stays inside the application test so the
// depguard rule keeps adapters out of the application layer.
type testStore struct{}

func (testStore) DefaultPath() string { return "" }

func (testStore) Read(path string) ([]byte, error) { return os.ReadFile(path) }

func (testStore) Load(string) (*domain.Config, error) { return nil, errors.New("not implemented") }

func (testStore) Save(string, *domain.Config) error { return errors.New("not implemented") }

func (testStore) Exists(string) (bool, error) { return false, errors.New("not implemented") }

func (testStore) SetInFile(string, string, string) error { return errors.New("not implemented") }

func (testStore) UnsetInFile(string, string) error { return errors.New("not implemented") }

type testEnv struct{}

func (testEnv) Getenv(key string) string { return os.Getenv(key) }

func Resolve(opts ResolveOptions) (*domain.ResolvedConfig, error) {
	return (&Service{Store: testStore{}, Env: testEnv{}}).Resolve(opts)
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
			"server.url":     "--server",
			"staff.url":      "--staff-url",
			"assets.bucket":  "--bucket",
			"defaults.scope": "--scope",
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
