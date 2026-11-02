package hostfs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/hostfs"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestConfigureShellIsIdempotent(t *testing.T) {
	home := t.TempDir()

	if err := hostfs.New(nil).ConfigureShell(home); err != nil {
		t.Fatalf("ConfigureShell failed: %v", err)
	}
	for _, rc := range []string{".zshrc", ".bashrc"} {
		content := readFile(t, filepath.Join(home, rc))
		if !strings.Contains(content, `export GOPRIVATE="git.internal.manova.space/*"`) {
			t.Errorf("expected GOPRIVATE in %s, got: %s", rc, content)
		}
		if !strings.Contains(content, `export GOPROXY="https://proxy.golang.org,direct"`) {
			t.Errorf("expected GOPROXY in %s, got: %s", rc, content)
		}
	}

	if err := hostfs.New(nil).ConfigureShell(home); err != nil {
		t.Fatalf("second ConfigureShell failed: %v", err)
	}
	for _, rc := range []string{".zshrc", ".bashrc"} {
		content := readFile(t, filepath.Join(home, rc))
		if strings.Count(content, "git.internal.manova.space") != 1 {
			t.Errorf("expected exactly 1 GOPRIVATE in %s, got %d", rc, strings.Count(content, "git.internal.manova.space"))
		}
		if strings.Count(content, "proxy.golang.org") != 1 {
			t.Errorf("expected exactly 1 GOPROXY in %s, got %d", rc, strings.Count(content, "proxy.golang.org"))
		}
	}
}

func TestConfigureShellPreservesExistingContent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(path, []byte("alias ll='ls -l'"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := hostfs.New(nil).ConfigureShell(home); err != nil {
		t.Fatalf("ConfigureShell failed: %v", err)
	}
	content := readFile(t, path)
	if !strings.HasPrefix(content, "alias ll='ls -l'\n") {
		t.Errorf("existing content lost or not terminated: %q", content)
	}
}

func TestWriteWireGuard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := hostfs.New(nil).WriteWireGuard("[Interface]\nPrivateKey = k\n")
	if err != nil {
		t.Fatalf("WriteWireGuard failed: %v", err)
	}
	want := filepath.Join(home, ".config", "orbit", "wg0.conf")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("wireguard config missing: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %v, want 0600", info.Mode().Perm())
	}
	if got := readFile(t, path); got != "[Interface]\nPrivateKey = k\n" {
		t.Errorf("content = %q", got)
	}
}

func TestConfigureMCPEnvRewritesTokens(t *testing.T) {
	workspace := t.TempDir()
	cursor := filepath.Join(workspace, ".cursor")
	if err := os.MkdirAll(cursor, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cursor, "mcp.env")
	if err := os.WriteFile(path, []byte("FORGEJO_TOKEN=old\nKEEP=value\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := hostfs.New(nil).ConfigureMCPEnv(workspace, "new-token", "dev-uid"); err != nil {
		t.Fatalf("ConfigureMCPEnv failed: %v", err)
	}

	content := readFile(t, path)
	for _, want := range []string{"FORGEJO_TOKEN=new-token", "FORGEJO_MCP_TOKEN=new-token", "MANOVA_USER_UID=dev-uid", "KEEP=value"} {
		if !strings.Contains(content, want) {
			t.Errorf("mcp.env missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "old") {
		t.Errorf("old token was not replaced:\n%s", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("mcp.env permissions = %v, want 0600", info.Mode().Perm())
	}
}

func TestConfigureMCPEnvCreatesFileFromTemplate(t *testing.T) {
	workspace := t.TempDir()
	if err := hostfs.New(nil).ConfigureMCPEnv(workspace, "token", "uid"); err != nil {
		t.Fatalf("ConfigureMCPEnv failed: %v", err)
	}
	content := readFile(t, filepath.Join(workspace, ".cursor", "mcp.env"))
	if !strings.Contains(content, "FORGEJO_TOKEN=token") || !strings.Contains(content, "MANOVA_USER_UID=uid") {
		t.Errorf("mcp.env missing credentials:\n%s", content)
	}
}

func TestTrustDevCA(t *testing.T) {
	t.Run("caddy missing", func(t *testing.T) {
		fake := &runnertest.Fake{}
		host := hostfs.New(fake)
		host.LookPath = func(string) (string, error) { return "", errors.New("not found") }

		present, trusted := host.TrustDevCA(context.Background())
		if present || trusted {
			t.Errorf("present=%v trusted=%v, want both false", present, trusted)
		}
		if len(fake.Calls()) != 0 {
			t.Errorf("unexpected commands: %+v", fake.Calls())
		}
	})

	t.Run("trust succeeds", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("caddy trust", runner.Result{}, nil)
		host := hostfs.New(fake)
		host.LookPath = func(string) (string, error) { return "/usr/bin/caddy", nil }

		present, trusted := host.TrustDevCA(context.Background())
		if !present || !trusted {
			t.Errorf("present=%v trusted=%v, want both true", present, trusted)
		}
		calls := fake.Calls()
		if len(calls) != 1 || calls[0].Name != "caddy" || strings.Join(calls[0].Args, " ") != "trust" {
			t.Errorf("unexpected calls: %+v", calls)
		}
	})

	t.Run("trust needs privileges", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("caddy trust", runner.Result{ExitCode: 1}, errors.New("permission denied"))
		host := hostfs.New(fake)
		host.LookPath = func(string) (string, error) { return "/usr/bin/caddy", nil }

		present, trusted := host.TrustDevCA(context.Background())
		if !present || trusted {
			t.Errorf("present=%v trusted=%v, want present and untrusted", present, trusted)
		}
	})
}

func TestExistsIsDirRemove(t *testing.T) {
	host := hostfs.New(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if !host.Exists(path) {
		t.Error("expected Exists to be true")
	}
	if host.IsDir(path) {
		t.Error("a file is not a directory")
	}
	if !host.IsDir(dir) {
		t.Error("expected IsDir to be true")
	}
	if err := host.Remove(path); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if host.Exists(path) {
		t.Error("expected the file to be removed")
	}
	if err := host.Remove(filepath.Join(dir, "does-not-exist")); err != nil {
		t.Errorf("removing a missing path is not an error: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
