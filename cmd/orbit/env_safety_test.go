package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvNoContractChecksPermissions(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	if err := os.Mkdir(filepath.Join(root, ".cursor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cursor", "mcp.env"), []byte("SECRET=canary\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := newEnvCheckCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{root})
	err := cmd.Execute()
	if err == nil {
		t.Error("absent contracts incorrectly successful")
	}
	if !strings.Contains(out.String(), "permissions") {
		t.Errorf("permission check skipped: %s", out)
	}
	if strings.Contains(out.String(), "✔ 0 schemas valid") {
		t.Error("absent contracts rendered as green validation")
	}
	if !strings.Contains(out.String(), "unverified") {
		t.Errorf("missing unverified status: %s", out)
	}
}

func TestEnvTraversalFailureSurfaces(t *testing.T) {
	cmd := newEnvCheckCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{filepath.Join(t.TempDir(), "missing")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("missing search root treated as success")
	}
}

func TestEnvCLIInvalidURLRedacted(t *testing.T) {
	root := t.TempDir()
	schema := "version: '1'\nvariables:\n - name: DATABASE_URL\n   type: url\n   required: true\n"
	for name, data := range map[string]string{".env.schema.yaml": schema, ".env": "DATABASE_URL=https://user:synthetic-cli-secret@bad host\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out := new(bytes.Buffer)
	cmd := newEnvCheckCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{root})
	if err := cmd.Execute(); err == nil {
		t.Fatal("invalid URL passed")
	}
	if strings.Contains(out.String(), "synthetic-cli-secret") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out.String(), "DATABASE_URL") {
		t.Fatalf("variable unidentified: %s", out)
	}
}
