package main

import (
	"bytes"
	"encoding/json"
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
		if res.Status != doctor.StatusOK && res.Status != doctor.StatusWarning && res.Status != doctor.StatusError {
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
