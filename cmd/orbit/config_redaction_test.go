package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cmdRedactionCanary = "tier1-cmd-canary-b7f1"

func writeRedactionCmdConfig(t *testing.T) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	body := "version: 2\ncustom:\n" +
		"  api_token: " + cmdRedactionCanary + "\n" +
		"  feature_tier: beta\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return cfgPath
}

func TestConfigGetMasksSecretUnlessRaw(t *testing.T) {
	cfgPath := writeRedactionCmdConfig(t)

	buf, err := execConfig(t, "get", "custom.api_token", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config get custom.api_token failed: %v", err)
	}
	if buf.String() != "********\n" {
		t.Errorf("config get without --raw = %q, want masked value", buf.String())
	}

	buf, err = execConfig(t, "get", "custom.api_token", "--raw", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config get --raw custom.api_token failed: %v", err)
	}
	if buf.String() != cmdRedactionCanary {
		t.Errorf("config get --raw = %q, want plaintext canary", buf.String())
	}

	buf, err = execConfig(t, "get", "custom.feature_tier", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config get custom.feature_tier failed: %v", err)
	}
	if buf.String() != "beta\n" {
		t.Errorf("config get non-secret = %q, want beta", buf.String())
	}
}

func TestConfigOutputPathsNeverLeakSecretCanary(t *testing.T) {
	cfgPath := writeRedactionCmdConfig(t)

	cases := [][]string{
		{"show", "--json"},
		{"show", "--format", "json"},
		{"show"},
		{"show", "--format", "yaml"},
		{"list", "--json"},
		{"list", "--format", "json"},
		{"list", "--format", "yaml"},
		{"list"},
		{"get", "custom.api_token", "--json"},
	}
	for _, args := range cases {
		buf, err := execConfig(t, append(args, "--config", cfgPath)...)
		if err != nil {
			t.Fatalf("config %v failed: %v", args, err)
		}
		if strings.Contains(buf.String(), cmdRedactionCanary) {
			t.Errorf("config %v leaked plaintext secret:\n%s", args, buf.String())
		}
		// The table view truncates the VALUE column ("**…"), so match the prefix.
		if !strings.Contains(buf.String(), "**") {
			t.Errorf("config %v did not render the mask:\n%s", args, buf.String())
		}
	}
}

func TestConfigSetDoesNotEchoSecret(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := execConfig(t, "init", "--config", cfgPath); err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	buf, err := execConfig(t, "set", "custom.api_token", cmdRedactionCanary, "--config", cfgPath)
	if err != nil {
		t.Fatalf("config set text failed: %v", err)
	}
	if strings.Contains(buf.String(), cmdRedactionCanary) {
		t.Errorf("config set text echoed secret:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "**") {
		t.Errorf("config set text did not render the mask:\n%s", buf.String())
	}

	buf, err = execConfig(t, "set", "custom.creds.client_secret", cmdRedactionCanary, "--json", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config set --json failed: %v", err)
	}
	if strings.Contains(buf.String(), cmdRedactionCanary) {
		t.Errorf("config set --json echoed secret:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `"value":"********"`) {
		t.Errorf("config set --json did not mask value field:\n%s", buf.String())
	}

	// The stored value is still the real one.
	buf, err = execConfig(t, "get", "custom.api_token", "--raw", "--config", cfgPath)
	if err != nil {
		t.Fatalf("config get --raw failed: %v", err)
	}
	if buf.String() != cmdRedactionCanary {
		t.Errorf("stored secret = %q, want canary", buf.String())
	}
}
