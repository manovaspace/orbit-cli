package domain

import (
	"crypto/ed25519"
	"crypto/rand"
	"reflect"
	"strings"
	"testing"

	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
	"golang.org/x/crypto/ssh"
)

// tunnelRegistry mixes shared services (one with a bind), a shared exception,
// and entries that must never be forwarded.
func tunnelRegistry() *portsdomain.Registry {
	return &portsdomain.Registry{
		Services: []portsdomain.Service{
			{Port: 10020, Name: "prometheus", Runs: "shared"},
			{Port: 10000, Name: "postgres", Runs: "shared"},
			{Port: 10006, Name: "nats", Runs: "shared", Bind: "10.0.0.1"},
			{Port: 10001, Name: "orbit-api-gateway", Runs: "mac", Localhost: "gateway"},
			{Port: 10003, Name: "dev-portal", Runs: "server"},
		},
		Exceptions: []portsdomain.Exception{
			{Port: 10334, Name: "redis", Runs: "shared"},
			{Port: 11006, Name: "immich", Runs: "server"},
		},
	}
}

func TestSharedForwardsSelectsSharedOnly(t *testing.T) {
	got := SharedForwards(tunnelRegistry())
	want := []Forward{
		{Name: "postgres", Port: 10000, Bind: "127.0.0.1"},
		{Name: "nats", Port: 10006, Bind: "10.0.0.1"},
		{Name: "prometheus", Port: 10020, Bind: "127.0.0.1"},
		{Name: "redis", Port: 10334, Bind: "127.0.0.1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SharedForwards = %+v, want %+v", got, want)
	}
}

func TestSelectForwardsOnlyFilters(t *testing.T) {
	got, err := SelectForwards(tunnelRegistry(), []string{"redis", "nats"})
	if err != nil {
		t.Fatalf("SelectForwards: %v", err)
	}
	if len(got) != 2 || got[0].Name != "nats" || got[1].Name != "redis" {
		t.Fatalf("filtered = %+v, want nats then redis in port order", got)
	}
}

func TestSelectForwardsUnknownNameListsValid(t *testing.T) {
	_, err := SelectForwards(tunnelRegistry(), []string{"gateway", "nope"})
	if err == nil {
		t.Fatal("want error for unknown names")
	}
	for _, want := range []string{"unknown tunnel service gateway, nope", "valid names: nats, postgres, prometheus, redis"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

func TestSelectForwardsNoSharedEntries(t *testing.T) {
	reg := &portsdomain.Registry{Services: []portsdomain.Service{{Port: 10001, Name: "gw", Runs: "mac"}}}
	if _, err := SelectForwards(reg, nil); err == nil {
		t.Fatal("want error when the registry has no shared entries")
	}
}

func TestLocalSpec(t *testing.T) {
	f := Forward{Name: "nats", Port: 10006, Bind: "10.0.0.1"}
	if got := f.LocalSpec(); got != "127.0.0.1:10006:10.0.0.1:10006" {
		t.Errorf("LocalSpec = %q", got)
	}
}

func TestSSHArgs(t *testing.T) {
	forwards := []Forward{
		{Name: "postgres", Port: 10000, Bind: "127.0.0.1"},
		{Name: "nats", Port: 10006, Bind: "10.0.0.1"},
	}
	got := SSHArgs("demo@203.0.113.10", 2222, "", forwards)
	want := []string{
		"ssh", "-N", "-p", "2222",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-L", "127.0.0.1:10000:127.0.0.1:10000",
		"-L", "127.0.0.1:10006:10.0.0.1:10006",
		"demo@203.0.113.10",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SSHArgs =\n%q\nwant\n%q", got, want)
	}
}

func TestParseTunnelHost(t *testing.T) {
	user, host, err := ParseTunnelHost("demo@203.0.113.10")
	if err != nil || user != "demo" || host != "203.0.113.10" {
		t.Fatalf("ParseTunnelHost = %q %q %v", user, host, err)
	}
	user, host, err = ParseTunnelHost("203.0.113.10")
	if err != nil || user != "" || host != "203.0.113.10" {
		t.Fatalf("bare host = %q %q %v", user, host, err)
	}
	for _, bad := range []string{"", "   ", "@203.0.113.10", "demo@", "de mo@203.0.113.10"} {
		if _, _, err := ParseTunnelHost(bad); err == nil {
			t.Errorf("ParseTunnelHost(%q): want error", bad)
		}
	}
}

func TestSSHConfigBlock(t *testing.T) {
	forwards := []Forward{
		{Name: "postgres", Port: 10000, Bind: "127.0.0.1"},
		{Name: "nats", Port: 10006, Bind: "10.0.0.1"},
	}
	got, err := SSHConfigBlock("demo@203.0.113.10", 2222, "", forwards)
	if err != nil {
		t.Fatalf("SSHConfigBlock: %v", err)
	}
	want := "Host orbit-dev\n" +
		"    HostName 203.0.113.10\n" +
		"    User demo\n" +
		"    Port 2222\n" +
		"    ExitOnForwardFailure yes\n" +
		"    ServerAliveInterval 30\n" +
		"    LocalForward 127.0.0.1:10000 127.0.0.1:10000\n" +
		"    LocalForward 127.0.0.1:10006 10.0.0.1:10006\n"
	if got != want {
		t.Fatalf("SSHConfigBlock =\n%s\nwant\n%s", got, want)
	}

	bare, err := SSHConfigBlock("203.0.113.10", 22, "", nil)
	if err != nil {
		t.Fatalf("bare host: %v", err)
	}
	if strings.Contains(bare, "User ") {
		t.Errorf("bare host block must not set User:\n%s", bare)
	}
}

func testPublicKey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	return line + " " + comment + "\n"
}

func TestAuthorizedKeyLine(t *testing.T) {
	keyFile := testPublicKey(t, "tunnel@laptop")
	forwards := []Forward{
		{Name: "postgres", Port: 10000, Bind: "127.0.0.1"},
		{Name: "nats", Port: 10006, Bind: "10.0.0.1"},
	}
	got, err := AuthorizedKeyLine(forwards, keyFile)
	if err != nil {
		t.Fatalf("AuthorizedKeyLine: %v", err)
	}
	keyLine := strings.TrimSpace(keyFile)
	want := `restrict,port-forwarding,permitopen="127.0.0.1:10000",permitopen="10.0.0.1:10006",command="echo orbit dev tunnel only" ` + keyLine
	if got != want {
		t.Fatalf("AuthorizedKeyLine =\n%s\nwant\n%s", got, want)
	}
}

func TestAuthorizedKeyLineRejectsBadFiles(t *testing.T) {
	valid := strings.TrimSpace(testPublicKey(t, "a"))
	tests := map[string]string{
		"empty":        "",
		"two keys":     valid + "\n" + valid + "\n",
		"not a key":    "hello world\n",
		"private key":  "-----BEGIN OPENSSH PRIVATE KEY-----\n",
		"with options": "no-pty " + valid + "\n",
	}
	for name, content := range tests {
		if _, err := AuthorizedKeyLine(nil, content); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestSSHArgsWithIdentity(t *testing.T) {
	forwards := []Forward{{Name: "postgres", Port: 10000, Bind: "127.0.0.1"}}
	got := SSHArgs("demo@203.0.113.10", 2222, "/home/demo/.ssh/orbit_dev_tunnel", forwards)
	want := []string{
		"ssh", "-N", "-p", "2222",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-i", "/home/demo/.ssh/orbit_dev_tunnel", "-o", "IdentitiesOnly=yes",
		"-L", "127.0.0.1:10000:127.0.0.1:10000",
		"demo@203.0.113.10",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SSHArgs with identity =\n%q\nwant\n%q", got, want)
	}
}

func TestSSHConfigBlockWithIdentity(t *testing.T) {
	forwards := []Forward{{Name: "postgres", Port: 10000, Bind: "127.0.0.1"}}
	got, err := SSHConfigBlock("demo@203.0.113.10", 2222, "/home/demo/.ssh/orbit_dev_tunnel", forwards)
	if err != nil {
		t.Fatalf("SSHConfigBlock: %v", err)
	}
	want := "Host orbit-dev\n" +
		"    HostName 203.0.113.10\n" +
		"    User demo\n" +
		"    Port 2222\n" +
		"    IdentityFile /home/demo/.ssh/orbit_dev_tunnel\n" +
		"    IdentitiesOnly yes\n" +
		"    ExitOnForwardFailure yes\n" +
		"    ServerAliveInterval 30\n" +
		"    LocalForward 127.0.0.1:10000 127.0.0.1:10000\n"
	if got != want {
		t.Fatalf("SSHConfigBlock with identity =\n%s\nwant\n%s", got, want)
	}
}

func TestExpandHome(t *testing.T) {
	tests := map[string]string{
		"~/.ssh/key":    "/home/demo/.ssh/key",
		"~":             "/home/demo",
		"/abs/key":      "/abs/key",
		"~other/key":    "~other/key",
		"relative/~key": "relative/~key",
	}
	for in, want := range tests {
		if got := ExpandHome(in, "/home/demo"); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ExpandHome("~/.ssh/key", ""); got != "~/.ssh/key" {
		t.Errorf("empty home: got %q", got)
	}
}
