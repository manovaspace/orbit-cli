package main

import (
	"bytes"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownScopesFail(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workspace.yaml")
	if err := os.WriteFile(path, []byte("version: '1'\nworkspace: fixture\ngroups:\n orbit:\n  path: orbit\n  repositories:\n   - name: fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, makeCmd := range []func() *cobra.Command{newStatusCmd, newSyncCmd, newRepairCmd} {
		cmd := makeCmd()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{"unknown-scope", "--manifest", path})
		if err := cmd.Execute(); err == nil {
			t.Errorf("%s unknown scope succeeded", cmd.Name())
		}
	}
}

func TestPortNoScanTruthful(t *testing.T) {
	cmd := newPortListCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--scan=false", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "[FREE]") || strings.Contains(out.String(), "in use") {
		t.Errorf("disabled scan claims listener knowledge: %s", out)
	}
	if !strings.Contains(out.String(), "unverified") {
		t.Errorf("disabled scan missing unverified: %s", out)
	}
}

func TestPortAssignmentIsNotReservation(t *testing.T) {
	cmd := newPortAllocateCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"0", "fixture"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not reserved") {
		t.Errorf("assignment implies reservation: %s", out)
	}
}
