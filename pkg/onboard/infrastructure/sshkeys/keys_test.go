package sshkeys_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/sshkeys"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestEnsureKeyReusesExistingKeyAndLoadsAgent(t *testing.T) {
	dir := t.TempDir()
	pub := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExisting dev@manova.space\n"
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519.pub"), []byte(pub), 0644); err != nil {
		t.Fatal(err)
	}

	fake := &runnertest.Fake{}
	fake.On("ssh-add", runner.Result{}, nil)

	keys := sshkeys.New(fake)
	keys.LookupEnv = func(string) string { return "/tmp/agent.sock" }

	got, err := keys.EnsureKey(dir, "dev@manova.space")
	if err != nil {
		t.Fatalf("EnsureKey failed: %v", err)
	}
	if got != strings.TrimSpace(pub) {
		t.Errorf("key = %q, want the existing public key", got)
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want one ssh-add", calls)
	}
	if calls[0].Name != "ssh-add" || len(calls[0].Args) != 1 || calls[0].Args[0] != filepath.Join(dir, "id_ed25519") {
		t.Errorf("unexpected ssh-add call: %+v", calls[0])
	}
}

func TestEnsureKeyGeneratesWithGoCryptoWhenSSHKeygenFails(t *testing.T) {
	dir := t.TempDir()
	fake := &runnertest.Fake{}
	fake.On("ssh-keygen", runner.Result{}, errors.New("ssh-keygen: not found"))

	keys := sshkeys.New(fake)
	keys.LookupEnv = func(string) string { return "" }

	got, err := keys.EnsureKey(dir, "dev@manova.space")
	if err != nil {
		t.Fatalf("EnsureKey failed: %v", err)
	}
	if !strings.HasPrefix(got, "ssh-ed25519 ") || !strings.HasSuffix(got, " dev@manova.space") {
		t.Errorf("unexpected generated key: %q", got)
	}

	// The fallback writes both files with the right permissions.
	info, err := os.Stat(filepath.Join(dir, "id_ed25519"))
	if err != nil {
		t.Fatalf("private key was not written: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("private key permissions = %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "id_ed25519.pub")); err != nil {
		t.Fatalf("public key was not written: %v", err)
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want the ssh-keygen attempt only", calls)
	}
	want := []string{"-t", "ed25519", "-C", "dev@manova.space", "-f", filepath.Join(dir, "id_ed25519"), "-N", ""}
	if strings.Join(calls[0].Args, "|") != strings.Join(want, "|") {
		t.Errorf("ssh-keygen args = %q, want %q", calls[0].Args, want)
	}
}

func TestEnsureKeyCreatesDirectoryAndSkipsAgentWithoutSocket(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", ".ssh")
	fake := &runnertest.Fake{}
	fake.On("ssh-keygen", runner.Result{}, errors.New("missing"))

	keys := sshkeys.New(fake)
	keys.LookupEnv = func(string) string { return "" }

	if _, err := keys.EnsureKey(dir, ""); err != nil {
		t.Fatalf("EnsureKey failed: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("ssh directory was not created: %v", err)
	}
	for _, call := range fake.Calls() {
		if call.Name == "ssh-add" {
			t.Errorf("ssh-add must not run without SSH_AUTH_SOCK: %+v", call)
		}
	}
}
