package application

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/config/domain"
)

const getRedactionCanary = "tier1-get-canary-4d8e"

func writeRedactionFixture(t *testing.T) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	body := "version: 2\ncustom:\n" +
		"  api_token: " + getRedactionCanary + "\n" +
		"  empty_token: \"\"\n" +
		"  feature_tier: beta\n" +
		"  creds:\n" +
		"    client_secret: " + getRedactionCanary + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return cfgPath
}

func TestGetMasksSecretUnlessRaw(t *testing.T) {
	cfgPath := writeRedactionFixture(t)
	svc := &Service{Store: testStore{}, Env: testEnv{}}

	entry, err := svc.Get(cfgPath, "custom.api_token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry.Value != domain.SecretMask {
		t.Errorf("Get custom.api_token = %q, want %q", entry.Value, domain.SecretMask)
	}

	entry, err = svc.Get(cfgPath, "custom.creds.client_secret")
	if err != nil {
		t.Fatalf("Get nested: %v", err)
	}
	if entry.Value != domain.SecretMask {
		t.Errorf("Get custom.creds.client_secret = %q, want %q", entry.Value, domain.SecretMask)
	}

	entry, err = svc.Get(cfgPath, "custom.empty_token")
	if err != nil {
		t.Fatalf("Get empty: %v", err)
	}
	if entry.Value != "" {
		t.Errorf("Get custom.empty_token = %q, want empty", entry.Value)
	}

	entry, err = svc.Get(cfgPath, "custom.feature_tier")
	if err != nil {
		t.Fatalf("Get feature_tier: %v", err)
	}
	if entry.Value != "beta" {
		t.Errorf("Get custom.feature_tier = %q, want beta", entry.Value)
	}
}

func TestGetRawReturnsPlaintextSecret(t *testing.T) {
	cfgPath := writeRedactionFixture(t)
	svc := &Service{Store: testStore{}, Env: testEnv{}}

	entry, err := svc.GetRaw(cfgPath, "custom.api_token")
	if err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if entry.Value != getRedactionCanary {
		t.Errorf("GetRaw custom.api_token = %q, want the plaintext value", entry.Value)
	}
}

func TestListMasksNestedSecretEntries(t *testing.T) {
	cfgPath := writeRedactionFixture(t)
	svc := &Service{Store: testStore{}, Env: testEnv{}}

	entries, err := svc.List(cfgPath)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := map[string]string{
		"custom.api_token":           domain.SecretMask,
		"custom.creds.client_secret": domain.SecretMask,
		"custom.empty_token":         "",
		"custom.feature_tier":        "beta",
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if strings.Contains(e.Value, getRedactionCanary) {
			t.Errorf("config list leaked plaintext for %s", e.Key)
		}
		if w, ok := want[e.Key]; ok {
			seen[e.Key] = true
			if e.Value != w {
				t.Errorf("config list %s = %q, want %q", e.Key, e.Value, w)
			}
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("config list missing entry %s", k)
		}
	}
}
