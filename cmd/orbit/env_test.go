package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	envapp "github.com/manovaspace/orbit-cli/pkg/env/application"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/spf13/cobra"
)

type noopMCP struct{}

func (noopMCP) Ensure(string) error { return nil }

func executeEnv(t *testing.T, command func() *cobra.Command, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	cmd := command()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return out, cmd.Execute()
}

func TestEnvCheckJSONEnvelope(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)
	if err := os.WriteFile(filepath.Join(root, ".env.schema.yaml"), []byte("version: '1'\nvariables:\n - name: PORT\n   type: integer\n   required: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PORT=10050\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := executeEnv(t, newEnvCheckCmd, root, "--json")
	if err != nil {
		t.Fatalf("env check --json: %v\n%s", err, out)
	}
	var envelope output.Envelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope.SchemaVersion != output.SchemaVersion || !envelope.OK {
		t.Fatalf("envelope = %+v", envelope)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var result envapp.CheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if !result.Verified || result.ValidCount != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestEnvSetupJSONEnvelope(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)
	previousMCP := envMCP
	envMCP = noopMCP{}
	t.Cleanup(func() { envMCP = previousMCP })
	if err := os.WriteFile(filepath.Join(root, ".env.schema.yaml"), []byte("version: '1'\nvariables:\n - name: PORT\n   type: integer\n   default: 10050\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := executeEnv(t, newEnvSetupCmd, root, "--json")
	if err != nil {
		t.Fatalf("env setup --json: %v\n%s", err, out)
	}
	var envelope output.Envelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope.SchemaVersion != output.SchemaVersion || !envelope.OK {
		t.Fatalf("envelope = %+v", envelope)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var result envapp.SetupResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if result.Generated != 1 {
		t.Fatalf("result = %+v", result)
	}
}
