package main

import (
	"strings"
	"testing"
)

func TestDoctorFixRefusedOnDarwin(t *testing.T) {
	prev := doctorGOOS
	doctorGOOS = "darwin"
	t.Cleanup(func() { doctorGOOS = prev })

	cmd := newDoctorCmd()
	cmd.SetArgs([]string{"--fix"})
	var stderr strings.Builder
	cmd.SetErr(&stderr)
	cmd.SetOut(&stderr)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not available on macOS") {
		t.Fatalf("doctor --fix on darwin: got err %v, want macOS refusal", err)
	}
}
