package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOrbitYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orbit.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadRepoSpecRoleDefaultAndNameRules(t *testing.T) {
	if _, ok, err := LoadRepoSpec(t.TempDir()); err != nil || ok {
		t.Fatalf("missing orbit.yaml: ok=%v err=%v", ok, err)
	}

	good := writeOrbitYAML(t, `
version: 1
toolchains: [go, bun]
services:
  postgres:
    databases:
      - name: falak_dev
      - name: falak_test
        role: falak_test_role
  mail: true
prepare: ["bun install"]
migrate: ["go run ./cmd/migrate"]
seed: []
env:
  - schema: .env.schema.yaml
    write: .env.local
run:
  web:
    cmd: "bun dev"
    port: "10010"
`)
	rs, ok, err := LoadRepoSpec(good)
	if err != nil || !ok {
		t.Fatalf("valid spec: ok=%v err=%v", ok, err)
	}
	if rs.Version != 1 || len(rs.Toolchains) != 2 || !rs.Services.Mail {
		t.Fatalf("fields not loaded: %+v", rs)
	}
	dbs := rs.Services.Postgres.Databases
	if len(dbs) != 2 || dbs[0].Role != "falak_dev" || dbs[1].Role != "falak_test_role" {
		t.Fatalf("role default: %+v", dbs)
	}
	if rs.Run["web"].Port != "10010" {
		t.Fatalf("run: %+v", rs.Run)
	}

	bad := map[string]string{
		"mixed-case name": "version: 1\nservices:\n  postgres:\n    databases:\n      - name: Falak-Dev\n",
		"bad role":        "version: 1\nservices:\n  postgres:\n    databases:\n      - name: falak\n        role: Bad-Role\n",
		"version":         "version: 2\n",
		"toolchain":       "version: 1\ntoolchains: [python]\n",
		"env escape":      "version: 1\nenv:\n  - schema: s\n    write: ../x\n",
		"env absolute":    "version: 1\nenv:\n  - schema: s\n    write: /etc/x\n",
	}
	for name, body := range bad {
		if _, _, err := LoadRepoSpec(writeOrbitYAML(t, body)); err == nil {
			t.Fatalf("%s: want error", name)
		}
	}
}

func TestLoadRepoSpecNameMessage(t *testing.T) {
	_, _, err := LoadRepoSpec(writeOrbitYAML(t, "version: 1\nservices:\n  postgres:\n    databases:\n      - name: Falak-Dev\n"))
	if err == nil || !strings.Contains(err.Error(), "Falak-Dev") {
		t.Fatalf("want error naming the database, got %v", err)
	}
}
