package application

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Tier-1 invariant 5 (secret redaction): every credential-like key must be
// masked as "********" in `config show` and `config list` output. The canary
// value must never appear in rendered output. Non-secret keys stay plaintext.
const redactionCanary = "tier1-canary-plaintext-7f3a"

func TestRedactionInvariantSecretKeysMasked(t *testing.T) {
	secretKeys := []string{
		"api_token",     // token
		"db_password",   // pass
		"client_secret", // secret
		"signing_key",   // key
		"auth_header",   // auth
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var b strings.Builder
	b.WriteString("version: 2\ncustom:\n")
	for _, k := range secretKeys {
		b.WriteString("  " + k + ": " + redactionCanary + "\n")
	}
	b.WriteString("  feature_tier: beta\n")
	if err := os.WriteFile(cfgPath, []byte(b.String()), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc := &Service{Store: testStore{}, Env: testEnv{}}

	t.Run("show masks secret keys in config and rendered YAML", func(t *testing.T) {
		cfg, err := svc.Show(cfgPath)
		if err != nil {
			t.Fatalf("Show: %v", err)
		}
		rendered, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		if strings.Contains(string(rendered), redactionCanary) {
			t.Errorf("config show leaked plaintext secret in YAML output:\n%s", rendered)
		}
		for _, k := range secretKeys {
			if got, _ := cfg.Custom[k].(string); got != "********" {
				t.Errorf("config show custom.%s = %q, want \"********\"", k, cfg.Custom[k])
			}
		}
		if got := cfg.Custom["feature_tier"]; got != "beta" {
			t.Errorf("non-secret custom.feature_tier = %v, want beta", got)
		}
	})

	t.Run("list masks secret entries", func(t *testing.T) {
		entries, err := svc.List(cfgPath)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		seen := map[string]bool{}
		for _, e := range entries {
			if strings.Contains(e.Value, redactionCanary) {
				t.Errorf("config list leaked plaintext for %s", e.Key)
			}
			for _, k := range secretKeys {
				if e.Key == "custom."+k {
					seen[k] = true
					if e.Value != "********" {
						t.Errorf("config list %s = %q, want \"********\"", e.Key, e.Value)
					}
				}
			}
		}
		for _, k := range secretKeys {
			if !seen[k] {
				t.Errorf("config list missing entry custom.%s", k)
			}
		}
	})
}
