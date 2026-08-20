package main

import (
	"bytes"
	"strings"
	"testing"
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
	if !strings.Contains(output, "Manova System Doctor") {
		t.Errorf("expected doctor output to contain header, got: %s", output)
	}

	// Verify check categories are printed
	if !strings.Contains(output, "System") && !strings.Contains(output, "Toolchain") {
		t.Errorf("expected doctor output to contain diagnostic categories, got: %s", output)
	}
}
