package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cifiles "github.com/manovaspace/orbit-cli/pkg/ci/infrastructure/files"
)

// ciFakeCommand records the CI command lines and standard input.
type ciFakeCommand struct {
	calls   []ciCall
	result  map[string]string
	failure map[string]error
}

type ciCall struct {
	stdin string
	argv  string
}

func newCIFakeCommand() *ciFakeCommand {
	return &ciFakeCommand{result: map[string]string{}, failure: map[string]error{}}
}

func (f *ciFakeCommand) Run(_ context.Context, stdin, _ string, name string, args ...string) (string, error) {
	argv := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, ciCall{stdin: stdin, argv: argv})
	if err, ok := f.failure[argv]; ok {
		return "", err
	}
	return f.result[argv], nil
}

const testCIManifest = `version: "1"
workspace: "test"
groups:
  test:
    path: ""
    repositories:
      - name: "orbit-tg"
        path: "orbit-tg"
        remote_url: "git@github.com:manovaspace/orbit-tg.git"
        ci_secrets:
          - NPM_TOKEN
`

func setupCITest(t *testing.T) (*ciFakeCommand, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)
	if err := os.WriteFile(filepath.Join(root, "workspace.yaml"), []byte(testCIManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	commands := newCIFakeCommand()
	prevCommands, prevFiles := ciCommands, ciFiles
	ciCommands, ciFiles = commands, cifiles.New()
	t.Cleanup(func() {
		ciCommands, ciFiles = prevCommands, prevFiles
	})
	return commands, root
}

func TestCIRunnerStatusJSON(t *testing.T) {
	commands, _ := setupCITest(t)
	commands.result["gh repo view manovaspace/orbit-tg --json visibility --jq .visibility"] = "PRIVATE\n"
	commands.result["systemctl is-active gh-ci"] = "active\n"
	commands.result["gh variable get CI_RUNNER -R manovaspace/orbit-tg --json value --jq .value"] = "vps\n"

	out, err := runRootForTest(t, "ci", "runner", "status", "--json")
	if err != nil {
		t.Fatalf("ci runner status --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Service string            `json:"service"`
		Vars    map[string]string `json:"vars"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Service != "active" {
		t.Errorf("service = %q", data.Service)
	}
	if data.Vars["manovaspace/orbit-tg"] != "vps" {
		t.Errorf("vars = %+v", data.Vars)
	}
}

func TestCIRunnerSetJSON(t *testing.T) {
	commands, _ := setupCITest(t)
	commands.result["gh repo view manovaspace/orbit-tg --json visibility --jq .visibility"] = "PRIVATE\n"

	out, err := runRootForTest(t, "ci", "runner", "github", "--json")
	if err != nil {
		t.Fatalf("ci runner github --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Mode         string   `json:"mode"`
		Repositories []string `json:"repositories"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Mode != "github" || len(data.Repositories) != 1 {
		t.Fatalf("data = %+v", data)
	}
	if last := commands.calls[len(commands.calls)-1].argv; last != "sudo systemctl stop gh-ci" {
		t.Errorf("last call = %q", last)
	}
}

func TestCIRunnerUnknownModeJSONIsUsage(t *testing.T) {
	setupCITest(t)

	out, err := runRootForTest(t, "ci", "runner", "carrier-pigeon", "--json")
	if err == nil {
		t.Fatal("expected an error")
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestCISecretsSyncJSONUsesStdin(t *testing.T) {
	commands, root := setupCITest(t)
	commands.result["gh repo view manovaspace/orbit-tg --json visibility --jq .visibility"] = "PRIVATE\n"

	source := filepath.Join(root, "ci-secrets.env")
	if err := os.WriteFile(source, []byte("NPM_TOKEN=s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runRootForTest(t, "ci", "secrets", "sync", "--source", source, "--keys-dir", filepath.Join(root, "keys"), "--json")
	if err != nil {
		t.Fatalf("ci secrets sync --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Repositories []struct {
			Name    string `json:"name"`
			Secrets int    `json:"secrets"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if len(data.Repositories) != 1 || data.Repositories[0].Name != "orbit-tg" || data.Repositories[0].Secrets != 1 {
		t.Fatalf("data = %+v", data)
	}

	var secret ciCall
	for _, c := range commands.calls {
		if strings.HasPrefix(c.argv, "gh secret set") {
			secret = c
		}
	}
	if secret.argv != "gh secret set NPM_TOKEN -R manovaspace/orbit-tg" {
		t.Fatalf("secret call = %+v", secret)
	}
	if secret.stdin != "s3cret" {
		t.Errorf("stdin = %q", secret.stdin)
	}
	if strings.Contains(secret.argv, "s3cret") {
		t.Error("secret value leaked into argv")
	}
}
