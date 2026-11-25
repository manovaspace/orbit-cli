package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/spf13/cobra"
)

func TestEnforceHost_Allowlist(t *testing.T) {
	t.Setenv("ORBIT_SKIP_HOSTGATE", "")
	t.Setenv("ORBIT_TESTBED", "")
	fail := func() host.Report {
		return host.Report{OK: false, Failures: []host.Failure{{Code: "os", Message: "nope"}}}
	}
	ok := func() host.Report { return host.Report{OK: true} }

	for _, name := range []string{"version", "doctor", "uninstall"} {
		cmd := &cobra.Command{Use: name}
		if err := enforceHost(cmd, fail); err != nil {
			t.Fatalf("%s should be allowlisted: %v", name, err)
		}
	}

	init := &cobra.Command{Use: "init"}
	if err := enforceHost(init, ok); err != nil {
		t.Fatalf("ok host should allow init: %v", err)
	}
	errBuf := &bytes.Buffer{}
	init.SetErr(errBuf)
	if err := enforceHost(init, fail); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "Orbit supports") {
		t.Fatalf("stderr: %s", errBuf.String())
	}
}

// hostTree builds orbit -> dev -> up, orbit -> ci -> runner -> vps, orbit -> port -> list,
// orbit -> pr -> status and orbit -> ci -> secrets so tests use real parent chains.
func hostTree() map[string]*cobra.Command {
	root := &cobra.Command{Use: "orbit"}
	cmds := map[string]*cobra.Command{}
	add := func(parent *cobra.Command, key, use string) {
		c := &cobra.Command{Use: use}
		parent.AddCommand(c)
		cmds[key] = c
	}
	add(root, "dev", "dev")
	add(cmds["dev"], "dev up", "up")
	add(cmds["dev"], "dev logs", "logs")
	add(root, "ci", "ci")
	add(cmds["ci"], "ci runner", "runner")
	add(cmds["ci runner"], "ci runner vps", "vps")
	add(cmds["ci"], "ci secrets", "secrets")
	add(root, "migrate", "migrate")
	add(cmds["migrate"], "migrate status", "status")
	add(root, "port", "port")
	add(cmds["port"], "port list", "list")
	add(root, "pr", "pr")
	add(cmds["pr"], "pr status", "status")
	add(root, "init", "init")
	return cmds
}

func darwinReport() host.Report {
	return host.Report{OK: true, Platform: "darwin"}
}

func linuxReport() host.Report {
	return host.Report{OK: true, Platform: "linux"}
}

func TestEnforceHost_DarwinServerOnlyRefused(t *testing.T) {
	t.Setenv("ORBIT_SKIP_HOSTGATE", "")
	t.Setenv("ORBIT_TESTBED", "")
	cmds := hostTree()
	for _, key := range []string{"dev up", "dev logs", "ci runner", "ci runner vps", "migrate status"} {
		err := enforceHost(cmds[key], darwinReport)
		if err == nil {
			t.Fatalf("%s must be refused on darwin", key)
		}
		want := "orbit " + key + ": server-only; not available on macOS"
		if err.Error() != want {
			t.Fatalf("%s: got %q want %q", key, err.Error(), want)
		}
		if clierr.ExitCode(err) != clierr.ExitUsage {
			t.Fatalf("%s: exit %d, want usage (%d)", key, clierr.ExitCode(err), clierr.ExitUsage)
		}
	}
}

func TestEnforceHost_DarwinDeveloperCommandsAllowed(t *testing.T) {
	t.Setenv("ORBIT_SKIP_HOSTGATE", "")
	t.Setenv("ORBIT_TESTBED", "")
	cmds := hostTree()
	for _, key := range []string{"port list", "pr status", "ci secrets", "init"} {
		if err := enforceHost(cmds[key], darwinReport); err != nil {
			t.Fatalf("%s must be allowed on darwin: %v", key, err)
		}
	}
}

func TestEnforceHost_LinuxAllowsServerCommands(t *testing.T) {
	t.Setenv("ORBIT_SKIP_HOSTGATE", "")
	t.Setenv("ORBIT_TESTBED", "")
	cmds := hostTree()
	for _, key := range []string{"dev up", "ci runner vps", "migrate status"} {
		if err := enforceHost(cmds[key], linuxReport); err != nil {
			t.Fatalf("%s must be allowed on linux: %v", key, err)
		}
	}
}

// TestHostGateAllowsSetupOnDarwinAndBash: a macOS host with a bash login shell and no ~/.local/bin
// on PATH passes the host check, and the setup, add, remove, projects, up, down, services and doctor
// commands are not refused by the gate.
func TestHostGateAllowsSetupOnDarwinAndBash(t *testing.T) {
	t.Setenv("ORBIT_SKIP_HOSTGATE", "")
	t.Setenv("ORBIT_TESTBED", "")
	report := host.Evaluate(host.Input{
		GOOS:       "darwin",
		GOARCH:     "arm64",
		OSRelease:  map[string]string{},
		Virt:       "native",
		LoginShell: "/bin/bash",
		Home:       "/Users/dev",
		Path:       "/usr/bin",
	})
	if !report.OK {
		t.Fatalf("darwin with bash must pass the host check: %#v", report.Failures)
	}
	root := newRootCmd()
	for _, name := range []string{"setup", "add", "remove", "projects", "up", "down", "services", "doctor"} {
		c, _, err := root.Find([]string{name})
		if err != nil || c == nil || c.Name() != name {
			t.Fatalf("command %s not found: %v", name, err)
		}
		if err := enforceHost(c, func() host.Report { return report }); err != nil {
			t.Fatalf("%s must be allowed on darwin: %v", name, err)
		}
	}
}

func TestEnforceHost_SkipBypassAllowsDarwinServerCommand(t *testing.T) {
	t.Setenv("ORBIT_SKIP_HOSTGATE", "1")
	t.Setenv("ORBIT_TESTBED", "")
	cmds := hostTree()
	if err := enforceHost(cmds["dev up"], darwinReport); err != nil {
		t.Fatalf("ORBIT_SKIP_HOSTGATE must bypass: %v", err)
	}
}

func TestCompletionZshOnly(t *testing.T) {
	root := newRootCmd()
	comp, _, err := root.Find([]string{"completion", "bash"})
	if err == nil && comp != nil && comp.Name() == "bash" {
		t.Fatal("bash completion must not be registered")
	}
	zsh, _, err := root.Find([]string{"completion", "zsh"})
	if err != nil || zsh == nil {
		t.Fatalf("zsh completion required: %v", err)
	}
}
