package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/dev/application"
	"golang.org/x/crypto/ssh"
)

// devTestRegistry is a workspace registry with two shared services, one shared
// exception, and one mac service that the tunnel must not forward.
const devTestRegistry = `version: 1
base: 10000
block_size: 50
fixed_slots: 10
projects:
  - { id: 0, name: orbit-platform }
services:
  - { port: 10000, project: orbit-platform, name: postgres, proto: tcp, runs: shared, repos: [orbit/orbit-infra] }
  - { port: 10006, project: orbit-platform, name: nats, proto: tcp, runs: shared, bind: 10.0.0.1, repos: [orbit/orbit-infra] }
  - { port: 10001, project: orbit-platform, name: orbit-api-gateway, proto: http, runs: mac, repos: [orbit/orbit-api-gateway], localhost: gateway }
exceptions:
  - { port: 10334, name: redis, runs: shared, repos: [orbit/orbit-infra], reason: "test", until: "test" }
`

// writeDevTestRoot writes a workspace root holding handbook/agents/workspace/ports.yaml.
func writeDevTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "handbook", "agents", "workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ports.yaml"), []byte(devTestRegistry), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// isolateDevConfig points the config file at a path with no content so that the
// developer's own dev.tunnel_host never leaks into a test.
func isolateDevConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ORBIT_CONFIG", path)
}

// fakeTunnelRunner records the argv the default path would run.
type fakeTunnelRunner struct {
	argv []string
	err  error
}

func (f *fakeTunnelRunner) Run(_ context.Context, argv []string) error {
	f.argv = argv
	return f.err
}

func useFakeTunnelRunner(t *testing.T) *fakeTunnelRunner {
	t.Helper()
	fake := &fakeTunnelRunner{}
	previous := devAttached
	devAttached = fake
	t.Cleanup(func() { devAttached = previous })
	return fake
}

func runDev(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

var devTunnelWantArgv = []string{
	"ssh", "-N", "-p", "2222",
	"-o", "ExitOnForwardFailure=yes",
	"-o", "ServerAliveInterval=30",
	"-o", "ServerAliveCountMax=3",
	"-L", "127.0.0.1:10000:127.0.0.1:10000",
	"-L", "127.0.0.1:10006:10.0.0.1:10006",
	"-L", "127.0.0.1:10334:127.0.0.1:10334",
	"demo@203.0.113.10",
}

func TestDevTunnelPrintShowsCommand(t *testing.T) {
	isolateDevConfig(t, "")
	code, out, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--ssh-port", "2222", "--print")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	want := strings.Join(devTunnelWantArgv, " ") + "\n"
	if out != want {
		t.Fatalf("print =\n%q\nwant\n%q", out, want)
	}
}

func TestDevTunnelUsesConfigHostAndPort(t *testing.T) {
	isolateDevConfig(t, "dev:\n  tunnel_host: demo@203.0.113.10\n  tunnel_port: 2222\n")
	code, out, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--print")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	if out != strings.Join(devTunnelWantArgv, " ")+"\n" {
		t.Fatalf("print from config = %q", out)
	}
}

func TestDevTunnelJSONIsPlanAndDoesNotRun(t *testing.T) {
	isolateDevConfig(t, "")
	fake := useFakeTunnelRunner(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--ssh-port", "2222", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	if fake.argv != nil {
		t.Fatalf("--json ran ssh: %q", fake.argv)
	}

	env := requireEnvelope(t, &stdout)
	var plan application.TunnelPlan
	if err := json.Unmarshal(env.Data, &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if plan.Host != "demo@203.0.113.10" || plan.Port != 2222 {
		t.Errorf("plan host/port = %q %d", plan.Host, plan.Port)
	}
	if len(plan.Forwards) != 3 || plan.Forwards[0].Name != "postgres" || plan.Forwards[1].Bind != "10.0.0.1" {
		t.Errorf("plan forwards = %+v", plan.Forwards)
	}
	if !reflect.DeepEqual(plan.Command, devTunnelWantArgv) {
		t.Errorf("plan command = %q", plan.Command)
	}
	data := envelopeData(t, env)
	if _, ok := data["forwards"]; !ok {
		t.Errorf("data keys = %v, want forwards", data)
	}
}

func TestDevTunnelMissingHostIsUsageError(t *testing.T) {
	isolateDevConfig(t, "")
	code, _, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--print")
	if code != clierr.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage); stderr: %s", code, clierr.ExitUsage, errOut)
	}
	if !strings.Contains(errOut, "no tunnel host: run orbit config set dev.tunnel_host user@host") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestDevTunnelMissingRegistryIsUnverified(t *testing.T) {
	isolateDevConfig(t, "")
	code, _, errOut := runDev(t, "dev", "tunnel", "--root", t.TempDir(), "--host", "demo@203.0.113.10")
	if code != clierr.ExitUnverified {
		t.Fatalf("exit = %d, want %d; stderr: %s", code, clierr.ExitUnverified, errOut)
	}
	if !strings.Contains(errOut, "no port registry at") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestDevTunnelUnknownOnlyIsUsageError(t *testing.T) {
	isolateDevConfig(t, "")
	code, _, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--only", "gateway", "--print")
	if code != clierr.ExitUsage {
		t.Fatalf("exit = %d, want usage; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "valid names: nats, postgres, redis") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestDevTunnelAuthorizedKeyBadFileIsUsageError(t *testing.T) {
	isolateDevConfig(t, "")
	bad := filepath.Join(t.TempDir(), "id.pub")
	if err := os.WriteFile(bad, []byte("not an ssh key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--authorized-key", bad)
	if code != clierr.ExitUsage {
		t.Fatalf("exit = %d, want usage; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, bad) {
		t.Errorf("stderr should name the file: %q", errOut)
	}
}

func TestDevTunnelAuthorizedKeyPrintsLine(t *testing.T) {
	isolateDevConfig(t, "")
	good := filepath.Join(t.TempDir(), "id.pub")
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshKey))) + " tunnel@laptop\n"
	if err := os.WriteFile(good, []byte(key), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--authorized-key", good)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	wantPrefix := `restrict,port-forwarding,permitopen="127.0.0.1:10000",permitopen="10.0.0.1:10006",permitopen="127.0.0.1:10334",command="echo orbit dev tunnel only" ssh-ed25519 `
	if !strings.HasPrefix(out, wantPrefix) || !strings.HasSuffix(out, "tunnel@laptop\n") {
		t.Errorf("authorized_keys line = %q", out)
	}
}

func TestDevTunnelSSHConfigBlock(t *testing.T) {
	isolateDevConfig(t, "")
	code, out, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--ssh-port", "2222", "--ssh-config")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	for _, want := range []string{"Host orbit-dev\n", "    HostName 203.0.113.10\n", "    User demo\n", "    Port 2222\n", "    LocalForward 127.0.0.1:10006 10.0.0.1:10006\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("block missing %q:\n%s", want, out)
		}
	}
}

func TestDevTunnelModesAreExclusive(t *testing.T) {
	isolateDevConfig(t, "")
	code, _, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--print", "--json")
	if code != clierr.ExitUsage {
		t.Fatalf("exit = %d, want usage; stderr: %s", code, errOut)
	}
}

func TestDevTunnelDefaultRunsInjectedRunner(t *testing.T) {
	isolateDevConfig(t, "")
	fake := useFakeTunnelRunner(t)
	code, _, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--ssh-port", "2222")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	if !reflect.DeepEqual(fake.argv, devTunnelWantArgv) {
		t.Fatalf("runner argv =\n%q\nwant\n%q", fake.argv, devTunnelWantArgv)
	}
}

func TestDevTunnelIdentityFlagAndConfig(t *testing.T) {
	isolateDevConfig(t, "")
	code, out, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--identity", "/keys/orbit", "--print")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	if !strings.Contains(out, " -i /keys/orbit -o IdentitiesOnly=yes -L ") {
		t.Errorf("print missing identity: %q", out)
	}
}

func TestDevTunnelIdentityFromConfigExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	isolateDevConfig(t, "dev:\n  tunnel_host: demo@203.0.113.10\n  tunnel_identity: ~/.ssh/orbit_dev_tunnel\n")
	code, out, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--print")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	want := " -i " + filepath.Join(home, ".ssh", "orbit_dev_tunnel") + " -o IdentitiesOnly=yes "
	if !strings.Contains(out, want) {
		t.Errorf("print = %q, want identity %q", out, want)
	}
}

func TestDevTunnelIdentityFlagOverridesConfig(t *testing.T) {
	isolateDevConfig(t, "dev:\n  tunnel_host: demo@203.0.113.10\n  tunnel_identity: /from/config\n")
	fake := useFakeTunnelRunner(t)
	code, _, errOut := runDev(t, "dev", "tunnel", "--root", writeDevTestRoot(t), "--identity", "/from/flag")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	joined := strings.Join(fake.argv, " ")
	if !strings.Contains(joined, "-i /from/flag ") || strings.Contains(joined, "/from/config") {
		t.Errorf("argv = %q, want flag identity only", joined)
	}
}

func TestDevTunnelJSONIncludesIdentity(t *testing.T) {
	isolateDevConfig(t, "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"dev", "tunnel", "--root", writeDevTestRoot(t), "--host", "demo@203.0.113.10", "--identity", "/keys/orbit", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	env := requireEnvelope(t, &stdout)
	var plan map[string]any
	if err := json.Unmarshal(env.Data, &plan); err != nil {
		t.Fatal(err)
	}
	if plan["identity"] != "/keys/orbit" {
		t.Errorf("identity = %v", plan["identity"])
	}
}
