package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const maskCanary = "tier1-masked-canary-91c2"

func TestMaskedDoesNotMutateOriginal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Custom = map[string]interface{}{
		"api_token": maskCanary,
		"nested": map[string]interface{}{
			"db_password": maskCanary,
			"plain":       "visible",
		},
		"list": []interface{}{
			map[string]interface{}{"secret": maskCanary},
		},
	}
	before := DefaultConfig()
	before.Custom = map[string]interface{}{
		"api_token": maskCanary,
		"nested": map[string]interface{}{
			"db_password": maskCanary,
			"plain":       "visible",
		},
		"list": []interface{}{
			map[string]interface{}{"secret": maskCanary},
		},
	}

	masked := cfg.Masked()

	if !reflect.DeepEqual(cfg, before) {
		t.Fatalf("Masked mutated the receiver:\n got: %#v\nwant: %#v", cfg, before)
	}
	if masked == cfg {
		t.Fatal("Masked returned the receiver pointer, want a copy")
	}
	if got := masked.Custom["api_token"]; got != SecretMask {
		t.Errorf("masked api_token = %v, want %q", got, SecretMask)
	}
}

func TestMaskedMasksNestedSecrets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Custom = map[string]interface{}{
		"api_token":   maskCanary,
		"feature":     "beta",
		"empty_token": "",
		"count":       3,
		"nested": map[string]interface{}{
			"db_password": maskCanary,
			"plain":       "visible",
		},
		"untyped": map[interface{}]interface{}{
			"signing_key": maskCanary,
			"label":       "shown",
		},
		"auth": map[string]interface{}{
			"header": maskCanary,
		},
		"items": []interface{}{
			map[string]interface{}{"client_secret": maskCanary, "name": "ok"},
		},
	}

	masked := cfg.Masked()

	if got := masked.Custom["api_token"]; got != SecretMask {
		t.Errorf("api_token = %v, want %q", got, SecretMask)
	}
	if got := masked.Custom["count"]; got != 3 {
		t.Errorf("non-string non-secret count = %v, want 3", got)
	}
	if got := masked.Custom["feature"]; got != "beta" {
		t.Errorf("feature = %v, want beta", got)
	}
	if got := masked.Custom["empty_token"]; got != "" {
		t.Errorf("empty secret = %q, want empty (not %q)", got, SecretMask)
	}
	nested, _ := masked.Custom["nested"].(map[string]interface{})
	if got := nested["db_password"]; got != SecretMask {
		t.Errorf("nested.db_password = %v, want %q", got, SecretMask)
	}
	if got := nested["plain"]; got != "visible" {
		t.Errorf("nested.plain = %v, want visible", got)
	}
	untyped, _ := masked.Custom["untyped"].(map[interface{}]interface{})
	if got := untyped["signing_key"]; got != SecretMask {
		t.Errorf("untyped.signing_key = %v, want %q", got, SecretMask)
	}
	if got := untyped["label"]; got != "shown" {
		t.Errorf("untyped.label = %v, want shown", got)
	}
	auth, _ := masked.Custom["auth"].(map[string]interface{})
	if got := auth["header"]; got != SecretMask {
		t.Errorf("auth.header under secret ancestor = %v, want %q", got, SecretMask)
	}
	items, _ := masked.Custom["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %v, want one element", items)
	}
	item, _ := items[0].(map[string]interface{})
	if got := item["client_secret"]; got != SecretMask {
		t.Errorf("items[0].client_secret = %v, want %q", got, SecretMask)
	}
	if got := item["name"]; got != "ok" {
		t.Errorf("items[0].name = %v, want ok", got)
	}

	rendered, err := yaml.Marshal(masked)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	jsonOut, err := json.Marshal(masked)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for name, out := range map[string]string{"yaml": string(rendered), "json": string(jsonOut)} {
		if strings.Contains(out, maskCanary) {
			t.Errorf("%s output leaked canary:\n%s", name, out)
		}
	}
}

func TestMaskedNilCustomStaysNil(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Custom = nil
	if got := cfg.Masked().Custom; got != nil {
		t.Errorf("Masked().Custom = %#v, want nil", got)
	}
}
