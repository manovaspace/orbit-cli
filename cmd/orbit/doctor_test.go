package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
)

func TestDoctorCmdExecution(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"doctor"})

	// Execute doctor command
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "Orbit System Doctor") {
		t.Errorf("expected doctor output to contain header, got: %s", output)
	}

	// Verify check categories are printed
	if !strings.Contains(output, "System") && !strings.Contains(output, "Toolchain") {
		t.Errorf("expected doctor output to contain diagnostic categories, got: %s", output)
	}
}

func TestDoctorCmdJSONExecution(t *testing.T) {
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs([]string{"doctor", "--json"})

	// Execute doctor command with --json flag
	_ = rootCmd.Execute()

	outputBytes := outBuf.Bytes()
	if len(outputBytes) == 0 {
		t.Fatalf("expected non-empty JSON output from doctor --json")
	}

	var report doctor.DoctorReport
	if err := json.Unmarshal(outputBytes, &report); err != nil {
		t.Fatalf("failed to unmarshal doctor --json output into doctor.DoctorReport: %v\nRaw output:\n%s", err, string(outputBytes))
	}

	if len(report.Results) == 0 {
		t.Errorf("expected diagnostic results in JSON report, got 0")
	}

	// Verify required diagnostic checks are present
	categories := make(map[string]bool)
	for _, res := range report.Results {
		categories[res.Category] = true
		if res.Name == "" {
			t.Errorf("diagnostic result missing Name: %+v", res)
		}
		if res.Status != doctor.StatusOK && res.Status != doctor.StatusWarning && res.Status != doctor.StatusError && res.Status != doctor.StatusUnverified {
			t.Errorf("invalid DiagnosticResult status %q in %+v", res.Status, res)
		}
	}

	expectedCategories := []string{"System", "Toolchain", "Runtime", "Container", "Authentication", "Ports", "Optional Tools"}
	for _, cat := range expectedCategories {
		if !categories[cat] {
			t.Errorf("expected JSON report to contain category %q", cat)
		}
	}
}

func TestDoctorCmdFlagsRegistration(t *testing.T) {
	cmd := newDoctorCmd()

	fixFlag := cmd.Flags().Lookup("fix")
	if fixFlag == nil {
		t.Fatal("expected --fix flag to be registered")
	}
	if fixFlag.Shorthand != "f" {
		t.Errorf("expected shorthand for --fix to be 'f', got %q", fixFlag.Shorthand)
	}

	jsonFlag := cmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Fatal("expected --json flag to be registered")
	}

	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil {
		t.Fatal("expected --yes flag to be registered")
	}
	if yesFlag.Shorthand != "y" {
		t.Errorf("expected shorthand for --yes to be 'y', got %q", yesFlag.Shorthand)
	}

	nonInteractiveFlag := cmd.Flags().Lookup("non-interactive")
	if nonInteractiveFlag == nil {
		t.Fatal("expected --non-interactive flag to be registered")
	}
}

func TestDoctorCmdFixFlagExecution(t *testing.T) {
	setupSafeDoctorTools(t)
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"doctor", "--fix", "--yes"})

	// Execute doctor command with --fix flag
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "Orbit System Doctor") {
		t.Errorf("expected doctor output to contain header, got: %s", output)
	}
}

func TestDoctorCmdFixFlagShorthandExecution(t *testing.T) {
	setupSafeDoctorTools(t)
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"doctor", "-f", "-y"})

	// Execute doctor command with -f shorthand flag
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "Orbit System Doctor") {
		t.Errorf("expected doctor output to contain header, got: %s", output)
	}
}

func TestDoctorCmdFixJSONExecution(t *testing.T) {
	setupSafeDoctorTools(t)
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs([]string{"doctor", "--fix", "--json"})

	// Execute doctor command with --fix and --json flags
	_ = rootCmd.Execute()

	outputBytes := outBuf.Bytes()
	if len(outputBytes) == 0 {
		t.Fatalf("expected non-empty JSON output from doctor --fix --json")
	}

	var report doctor.DoctorReport
	if err := json.Unmarshal(outputBytes, &report); err != nil {
		t.Fatalf("failed to unmarshal doctor --fix --json output: %v\nRaw output:\n%s", err, string(outputBytes))
	}
}

func TestDoctorCmdInteractivePromptDecline(t *testing.T) {
	buf := new(bytes.Buffer)
	in := bytes.NewBufferString("n\n")

	rootCmd := newRootCmd()
	rootCmd.SetIn(in)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"doctor"})

	// Execute doctor with simulated "no" to healing prompt
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "Orbit System Doctor") {
		t.Errorf("expected doctor output to contain header, got: %s", output)
	}
}

func TestLocalDoctorRejectsMutation(t *testing.T) {
	for _, args := range [][]string{{"--local", "--fix"}, {"--local", "--remote"}, {"--accept-host-keys"}} {
		cmd := newDoctorCmd()
		cmd.SetArgs(args)
		cmd.SetOut(new(bytes.Buffer))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted unsafe combination %v", args)
		}
	}
	if !shouldSuppressPostRunNotices(newDoctorCmd()) {
		t.Fatal("doctor permits updater side effects")
	}
}

// Keep explicit repair tests isolated from workstation tool installation.
func setupSafeDoctorTools(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("SSH_AUTH_SOCK", "")
	for tool, output := range map[string]string{"git": "git version 2.50.0", "go": "go version go1.26.7 linux/amd64", "node": "v24.0.0", "bun": "1.4.0", "docker": "24.0.0", "caddy": "2.10.0", "typst": "0.14.0"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\necho '"+output+"'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkspaceDoctorDoesNotRefreshGitIndex(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	t.Chdir(root)
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(root, "workspace.yaml"), []byte("version: '1'\nworkspace: fixture\ngroups:\n orbit:\n  path: orbit\n  repositories:\n   - name: fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "orbit", "fixture", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "orbit", "fixture", ".git", "index-refresh")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho touched > '"+marker+"'\necho true\n"), 0755); err != nil {
		t.Fatal(err)
	}
	addWorkspaceGitDiagnostics(doctor.NewReport())
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("structural workspace diagnostics invoked git and mutated repo")
	}
}

func TestWorkspaceDoctorInvalidManifestFailsReadiness(t *testing.T) {
	for _, kind := range []string{"missing", "invalid", "directory", "empty"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			path := filepath.Join(root, "workspace.yaml")
			switch kind {
			case "invalid":
				if err := os.WriteFile(path, []byte("groups: [invalid\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "empty":
				if err := os.WriteFile(path, []byte("version: '1'\nworkspace: empty\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			report := doctor.NewReport()
			addWorkspaceGitDiagnostics(report)
			if !report.HasErrors() {
				t.Fatalf("unvalidated workspace treated as ready: %+v", report.Results)
			}
			for _, result := range report.Results {
				if result.Name == "Manifest" && strings.Contains(result.Message, "unverified") {
					return
				}
			}
			t.Fatal("missing explicit unverified manifest diagnostic")
		})
	}
}
