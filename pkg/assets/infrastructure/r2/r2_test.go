package r2

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r2.env")
	body := "R2_ACCOUNT_ID=abc\nR2_ACCESS_KEY_ID=kid\nR2_SECRET_ACCESS_KEY=sec\nR2_BUCKET=manova-assets\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccountID != "abc" || cfg.AccessKeyID != "kid" || cfg.SecretAccessKey != "sec" || cfg.Bucket != "manova-assets" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Endpoint != "https://abc.r2.cloudflarestorage.com" {
		t.Fatalf("endpoint %s", cfg.Endpoint)
	}
}

func TestLoadEnvMissingRequired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r2.env")
	if err := os.WriteFile(path, []byte("R2_ACCOUNT_ID=abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEnv(path); err == nil {
		t.Fatal("expected error")
	}
}
