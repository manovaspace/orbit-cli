package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/output"
	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	"github.com/spf13/cobra"
)

func executePort(t *testing.T, command func() *cobra.Command, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	cmd := command()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return out, cmd.Execute()
}

func TestPortListJSONEnvelope(t *testing.T) {
	out, err := executePort(t, newPortListCmd, "--json", "--limit", "1")
	if err != nil {
		t.Fatalf("port list --json: %v\n%s", err, out)
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
	var result portsapp.ListResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(result.Projects) != 1 || result.TotalProjects == 0 {
		t.Fatalf("result = %+v", result)
	}
	if result.Scanned {
		t.Error("list reported a scan it did not run")
	}
}

func TestPortAllocateJSONEnvelope(t *testing.T) {
	out, err := executePort(t, newPortAllocateCmd, "orbit-platform", "fixture", "--json")
	if err != nil {
		t.Fatalf("port allocate --json: %v\n%s", err, out)
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
	var result portsapp.AllocateResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if result.Port < 10010 || result.Port > 10049 || result.Reserved {
		t.Fatalf("result = %+v", result)
	}
}
