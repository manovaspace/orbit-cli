package main

import (
	"os"
	"testing"
)

// TestMain keeps command tests independent of the machine they run on. The
// host gate refuses most commands on hosts that are not deployment targets
// (CI runners, containers), which is correct for users but would make the
// command tests fail on GitHub Actions. Tests that exercise the gate itself
// clear the override locally.
func TestMain(m *testing.M) {
	if os.Getenv("ORBIT_SKIP_HOSTGATE") == "" && os.Getenv("ORBIT_TESTBED") == "" {
		_ = os.Setenv("ORBIT_SKIP_HOSTGATE", "1")
	}
	os.Exit(m.Run())
}
