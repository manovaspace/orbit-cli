package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/spf13/cobra"
)

func TestEnforceHost_Allowlist(t *testing.T) {
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
	if !strings.Contains(errBuf.String(), "Orbit requires") {
		t.Fatalf("stderr: %s", errBuf.String())
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
