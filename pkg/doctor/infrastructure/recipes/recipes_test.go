package recipes

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

// fakeShell records scripts and resolves sudo as available.
type fakeShell struct {
	scripts  []string
	shellErr error
	output   string
	noSudo   bool
}

func (s *fakeShell) RunShell(_ context.Context, script string) (string, error) {
	s.scripts = append(s.scripts, script)
	return s.output, s.shellErr
}

func (s *fakeShell) LookPath(name string) (string, error) {
	if s.noSudo && name == "sudo" {
		return "", errors.New("not found")
	}
	return "/usr/bin/" + name, nil
}

func (s *fakeShell) last() string {
	if len(s.scripts) == 0 {
		return ""
	}
	return s.scripts[len(s.scripts)-1]
}

func TestDefaultRegistryNames(t *testing.T) {
	reg := NewDefaultRegistry(runner.Exec{})
	names := []string{"Go 1.26", "Bun", "Node.js 24 LTS", "Git", "Docker Compose", "Docker Daemon", "Caddy Reverse Proxy", "Typst Compiler", "Zsh & Shell Profile"}
	if len(reg.All()) != len(names) {
		t.Fatalf("healers = %d, want %d", len(reg.All()), len(names))
	}
	for _, name := range names {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("missing healer %q", name)
		}
	}
}

func TestGoHealerScripts(t *testing.T) {
	ctx := context.Background()
	t.Run("non-root", func(t *testing.T) {
		shell := &fakeShell{}
		h := &GoHealer{Shell: shell, Version: "1.26.4", GOOS: "linux", GOARCH: "amd64", IsRoot: func() bool { return false }}
		if err := h.Heal(ctx, nil); err != nil {
			t.Fatal(err)
		}
		script := shell.last()
		if !strings.Contains(script, "https://go.dev/dl/go1.26.4.linux-amd64.tar.gz") || !strings.Contains(script, "sudo tar -C /usr/local") || !strings.Contains(script, "/etc/profile.d/orbit-go.sh") {
			t.Errorf("script = %s", script)
		}
	})
	t.Run("unsupported arch", func(t *testing.T) {
		h := &GoHealer{Shell: &fakeShell{}, GOOS: "linux", GOARCH: "arm64"}
		if err := h.Heal(ctx, nil); err == nil || !strings.Contains(err.Error(), "unsupported architecture") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unsupported os", func(t *testing.T) {
		h := &GoHealer{Shell: &fakeShell{}, GOOS: "darwin", GOARCH: "amd64"}
		if err := h.Heal(ctx, nil); err == nil || !strings.Contains(err.Error(), "supported on Linux only") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("execution failure", func(t *testing.T) {
		h := &GoHealer{Shell: &fakeShell{shellErr: errors.New("tar failed"), output: "disk full"}, GOOS: "linux", GOARCH: "amd64", IsRoot: func() bool { return true }}
		if err := h.Heal(ctx, nil); err == nil || !strings.Contains(err.Error(), "failed to install Go") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no sudo", func(t *testing.T) {
		h := &GoHealer{Shell: &fakeShell{noSudo: true}, GOOS: "linux", GOARCH: "amd64", IsRoot: func() bool { return false }}
		if err := h.Heal(ctx, nil); err == nil || !strings.Contains(err.Error(), "root privileges") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestBunHealer(t *testing.T) {
	h := NewBunHealer(&fakeShell{})
	if !h.CanHeal(domain.DiagnosticResult{Name: "Bun", Status: domain.StatusError}) {
		t.Error("Bun error not matched")
	}
	if h.CanHeal(domain.DiagnosticResult{Name: "Bun", Status: domain.StatusOK}) {
		t.Error("Bun OK matched")
	}
	shell := &fakeShell{}
	h = NewBunHealer(shell)
	if err := h.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shell.last(), "https://bun.sh/install") {
		t.Errorf("script = %s", shell.last())
	}
}

func TestNodeHealer(t *testing.T) {
	shell := &fakeShell{}
	h := &NodeHealer{Shell: shell, IsRoot: func() bool { return false }}
	if err := h.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	script := shell.last()
	if !strings.Contains(script, "setup_24.x") || !strings.Contains(script, "sudo -E bash -") || !strings.Contains(script, "sudo apt-get install -y nodejs") {
		t.Errorf("script = %s", script)
	}

	shell = &fakeShell{}
	h = &NodeHealer{Shell: shell, IsRoot: func() bool { return true }}
	if err := h.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(shell.last(), "sudo ") {
		t.Errorf("root script used sudo: %s", shell.last())
	}

	h = &NodeHealer{Shell: &fakeShell{shellErr: errors.New("apt failure")}, IsRoot: func() bool { return true }}
	if err := h.Heal(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "failed to install Node.js 24") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitHealer(t *testing.T) {
	h := NewGitHealer(&fakeShell{})
	if !h.CanHeal(domain.DiagnosticResult{Name: "Git", Status: domain.StatusError}) {
		t.Error("Git error not matched")
	}
	if h.CanHeal(domain.DiagnosticResult{Name: "GitHub SSH (github.com)", Status: domain.StatusWarning}) {
		t.Error("GitHub SSH matched Git healer")
	}
	shell := &fakeShell{}
	h = &GitHealer{Shell: shell, IsRoot: func() bool { return false }}
	if err := h.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shell.last(), "sudo apt-get install -y git ca-certificates") {
		t.Errorf("script = %s", shell.last())
	}
}

func TestDockerHealers(t *testing.T) {
	composeShell := &fakeShell{}
	compose := &DockerComposeHealer{Shell: composeShell, IsRoot: func() bool { return false }}
	if err := compose.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(composeShell.last(), "sudo apt-get install -y docker-compose-plugin") {
		t.Errorf("compose script = %s", composeShell.last())
	}

	daemonShell := &fakeShell{}
	daemon := &DockerDaemonHealer{Shell: daemonShell, IsRoot: func() bool { return false }}
	if !daemon.CanHeal(domain.DiagnosticResult{Name: "Docker Daemon", Status: domain.StatusError}) {
		t.Error("Docker Daemon not matched")
	}
	if err := daemon.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(daemonShell.last(), "apt-get install -y docker") {
		t.Errorf("daemon script = %s", daemonShell.last())
	}
}

func TestCaddyTypstAndZshHealers(t *testing.T) {
	caddyShell := &fakeShell{}
	caddy := &CaddyHealer{Shell: caddyShell, IsRoot: func() bool { return false }}
	if !caddy.CanHeal(domain.DiagnosticResult{Name: "Caddy Reverse Proxy", Status: domain.StatusWarning}) {
		t.Error("Caddy not matched")
	}
	if err := caddy.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(caddyShell.last(), "apt-get install -y caddy") {
		t.Errorf("caddy script = %s", caddyShell.last())
	}

	typstShell := &fakeShell{}
	typst := &TypstHealer{Shell: typstShell, IsRoot: func() bool { return false }}
	if err := typst.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(typstShell.last(), "typst") {
		t.Errorf("typst script = %s", typstShell.last())
	}

	zshShell := &fakeShell{}
	zsh := &ZshHealer{Shell: zshShell, IsRoot: func() bool { return false }}
	if !zsh.CanHeal(domain.DiagnosticResult{Name: "Host", Status: domain.StatusError, Message: "chsh to zsh"}) {
		t.Error("Zsh not matched")
	}
	if err := zsh.Heal(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(zshShell.last(), "apt-get install -y zsh") {
		t.Errorf("zsh script = %s", zshShell.last())
	}
}

func TestRunnerShellUsesBashScript(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("bash -c echo install", runner.Result{Stdout: "installed", Stderr: "\n"}, nil)

	out, err := (RunnerShell{Runner: fake}).RunShell(context.Background(), "echo install")
	if err != nil {
		t.Fatal(err)
	}
	if out != "installed\n" {
		t.Errorf("output = %q", out)
	}
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Name != "bash" || !reflect.DeepEqual(calls[0].Args, []string{"-c", "echo install"}) {
		t.Fatalf("calls = %+v", calls)
	}
}
