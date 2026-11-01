package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/output"
)

func requireEnvelope(t *testing.T, buf *bytes.Buffer) prEnvelope {
	t.Helper()
	env := decodeEnvelope(t, buf.String())
	if env.SchemaVersion != output.SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", env.SchemaVersion, output.SchemaVersion)
	}
	if !env.OK {
		t.Errorf("envelope not ok: %+v", env)
	}
	if len(env.Data) == 0 {
		t.Error("expected data in envelope")
	}
	return env
}

func envelopeData(t *testing.T, env prEnvelope) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode envelope data: %v", err)
	}
	return data
}

func writeJSONManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", dir)
	path := filepath.Join(dir, "workspace.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStatusJSONEnvelope(t *testing.T) {
	path := writeJSONManifest(t, `version: "1"
workspace: test
groups:
  test:
    repositories:
      - name: missing
        path: missing
`)
	out := &bytes.Buffer{}
	cmd := newStatusCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all", "--manifest", path, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	data := envelopeData(t, requireEnvelope(t, out))
	if data["scope"] != "all" {
		t.Errorf("scope = %v", data["scope"])
	}
}

func TestSyncJSONEnvelope(t *testing.T) {
	path := writeJSONManifest(t, "version: \"1\"\nworkspace: test\ngroups: {}\n")
	out := &bytes.Buffer{}
	cmd := newSyncCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all", "--manifest", path, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("sync --json: %v", err)
	}
	requireEnvelope(t, out)
}

func TestRepairJSONEnvelope(t *testing.T) {
	path := writeJSONManifest(t, "version: \"1\"\nworkspace: test\ngroups: {}\n")
	out := &bytes.Buffer{}
	cmd := newRepairCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"all", "--manifest", path, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repair --json: %v", err)
	}
	requireEnvelope(t, out)
}

func TestInitJSONEnvelope(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)
	out := &bytes.Buffer{}
	cmd := newInitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--bootstrap", "--manifest", filepath.Join(root, "absent.yaml"), "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --json: %v", err)
	}
	data := envelopeData(t, requireEnvelope(t, out))
	if data["bootstrap"] != true {
		t.Errorf("bootstrap = %v", data["bootstrap"])
	}
}

func TestUpdateJSONEnvelope(t *testing.T) {
	t.Setenv("ORBIT_WORKSPACE", t.TempDir())
	out := &bytes.Buffer{}
	cmd := newUpdateCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--skip-selfupdate", "--skip-sync", "--skip-migrate", "--skip-env", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --json: %v", err)
	}
	requireEnvelope(t, out)
}
