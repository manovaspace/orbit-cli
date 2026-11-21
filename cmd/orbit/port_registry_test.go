package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	"github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/registry"
)

// portFixtureRoot is a workspace root with handbook/agents/workspace/ports.yaml
// and one example of every scanned file kind (see pkg/ports/infrastructure/scan).
const portFixtureRoot = "../../pkg/ports/infrastructure/scan/testdata/checkroot"

// freePortProbe reports every port free, so allocation is deterministic on any host.
type freePortProbe struct{}

func (freePortProbe) Available(int) bool         { return true }
func (freePortProbe) Scan(int, int) map[int]bool { return map[int]bool{} }

func useFreeProbe(t *testing.T) {
	t.Helper()
	previous := portProbe
	portProbe = freePortProbe{}
	t.Cleanup(func() { portProbe = previous })
}

func decodePortEnvelope(t *testing.T, out *bytes.Buffer) output.Envelope {
	t.Helper()
	var envelope output.Envelope
	// The buffer also holds cobra's "Error:" line on failure; decode only the envelope.
	if err := json.NewDecoder(out).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	return envelope
}

func TestPortUnknownSubcommandIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"port", "bogus"}, &stdout, &stderr)
	if code != clierr.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)\nstderr: %s", code, clierr.ExitUsage, stderr.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "bogus"`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestPortAllocAliasSkipsRegistryClaims(t *testing.T) {
	useFreeProbe(t)
	out, err := executePort(t, newPortCmd, "alloc", "orbit-platform", "fixture", "--root", portFixtureRoot, "--json")
	if err != nil {
		t.Fatalf("port alloc: %v\n%s", err, out)
	}
	envelope := decodePortEnvelope(t, out)
	data, _ := json.Marshal(envelope.Data)
	var result portsapp.AllocateResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	// 10010-10032 are registered and 10026 is retired, so the lowest free slot is 10033.
	if result.Port != 10033 || result.Reserved {
		t.Fatalf("result = %+v, want port 10033", result)
	}
}

func TestPortAllocateTextAsksForRegistryEntry(t *testing.T) {
	useFreeProbe(t)
	out, err := executePort(t, newPortAllocateCmd, "orbit-platform", "fixture", "--root", portFixtureRoot)
	if err != nil {
		t.Fatalf("port allocate: %v\n%s", err, out)
	}
	if !strings.Contains(out.String(), "add it to handbook/agents/workspace/ports.yaml") {
		t.Fatalf("missing registry hint:\n%s", out)
	}
}

func TestPortAllocateRejectsProjectPastMaxPort(t *testing.T) {
	useFreeProbe(t)
	_, err := executePort(t, newPortAllocateCmd, "1110", "fixture", "--root", portFixtureRoot)
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestPortListWithRegistryIsServiceTable(t *testing.T) {
	useFreeProbe(t)
	out, err := executePort(t, newPortListCmd, "--root", portFixtureRoot, "--json")
	if err != nil {
		t.Fatalf("port list: %v\n%s", err, out)
	}
	envelope := decodePortEnvelope(t, out)
	data, _ := json.Marshal(envelope.Data)
	var result portsapp.ServiceListResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Probed || result.TotalServices == 0 || len(result.Services) != result.TotalServices {
		t.Fatalf("result = %+v", result)
	}
	first := result.Services[0]
	if first.Port != 10000 || first.Service != "postgres" || first.Project != "orbit-platform" || len(first.Repos) == 0 {
		t.Fatalf("first service = %+v", first)
	}
	for i := 1; i < len(result.Services); i++ {
		if result.Services[i].Port <= result.Services[i-1].Port {
			t.Fatalf("services not sorted by port at %d", i)
		}
	}
}

func TestPortListWithRegistryPagesServices(t *testing.T) {
	useFreeProbe(t)
	out, err := executePort(t, newPortListCmd, "--root", portFixtureRoot, "--limit", "5", "--page", "2")
	if err != nil {
		t.Fatalf("port list: %v\n%s", err, out)
	}
	for _, want := range []string{"Port registry", "showing 6-10 of 55"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
}

func TestPortListWithoutRegistryKeepsProjectBlocks(t *testing.T) {
	useFreeProbe(t)
	out, err := executePort(t, newPortListCmd, "--root", t.TempDir(), "--json", "--limit", "1")
	if err != nil {
		t.Fatalf("port list: %v\n%s", err, out)
	}
	envelope := decodePortEnvelope(t, out)
	data, _ := json.Marshal(envelope.Data)
	var result portsapp.ListResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Projects) != 1 || result.TotalProjects == 0 {
		t.Fatalf("fallback result = %+v", result)
	}
}

func TestPortListMalformedRegistryFails(t *testing.T) {
	useFreeProbe(t)
	root := t.TempDir()
	dir := filepath.Join(root, "handbook", "agents", "workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ports.yaml"), []byte("services: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := executePort(t, newPortListCmd, "--root", root, "--json")
	if err == nil {
		t.Fatalf("malformed registry accepted:\n%s", out)
	}
	if clierr.ExitCode(err) != clierr.ExitFailed {
		t.Fatalf("exit = %d, want %d", clierr.ExitCode(err), clierr.ExitFailed)
	}
}

func TestPortCheckFindingsExitOne(t *testing.T) {
	out, err := executePort(t, newPortCheckCmd, "--root", portFixtureRoot, "--json")
	if err == nil {
		t.Fatalf("findings must fail the command:\n%s", out)
	}
	if clierr.ExitCode(err) != clierr.ExitFailed {
		t.Fatalf("exit = %d, want %d", clierr.ExitCode(err), clierr.ExitFailed)
	}
	envelope := decodePortEnvelope(t, out)
	if envelope.OK {
		t.Fatal("envelope ok = true with findings")
	}
	data, _ := json.Marshal(envelope.Data)
	var result portsapp.CheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"UNREGISTERED": 4, "RETIRED": 2, "WRONG_REPO": 1, "RESERVED": 1, "OPEN_BIND": 4}
	for kind, n := range want {
		if result.Counts[kind] != n {
			t.Errorf("count %s = %d, want %d", kind, result.Counts[kind], n)
		}
	}
	if result.Failing != 8 || result.Warnings != 4 {
		t.Errorf("failing = %d warnings = %d", result.Failing, result.Warnings)
	}
}

func TestPortCheckTextGroupsByRepository(t *testing.T) {
	out, err := executePort(t, newPortCheckCmd, "--root", portFixtureRoot)
	if clierr.ExitCode(err) != clierr.ExitFailed {
		t.Fatalf("exit = %d, want 1: %v", clierr.ExitCode(err), err)
	}
	text := out.String()
	for _, want := range []string{"clients/manova/waypoint-api", "orbit/orbit-infra", "WRONG_REPO", "UNREGISTERED", "RESERVED"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
}

func TestPortCheckCleanRootPasses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "handbook", "agents", "workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(portFixtureRoot, "handbook", "agents", "workspace", "ports.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ports.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := executePort(t, newPortCheckCmd, "--root", root, "--json")
	if err != nil {
		t.Fatalf("clean root failed: %v\n%s", err, out)
	}
	if !decodePortEnvelope(t, out).OK {
		t.Fatalf("envelope not ok:\n%s", out)
	}
}

func TestPortCheckWithoutRegistryIsUnverified(t *testing.T) {
	out, err := executePort(t, newPortCheckCmd, "--root", t.TempDir(), "--json")
	if clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Fatalf("exit = %d, want %d: %v", clierr.ExitCode(err), clierr.ExitUnverified, err)
	}
	if decodePortEnvelope(t, out).OK {
		t.Fatal("missing registry must not report ok")
	}
}

func TestRegistryNotFoundIsTyped(t *testing.T) {
	_, err := registry.Find(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}
