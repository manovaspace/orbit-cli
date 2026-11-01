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
//
// The git config overrides keep test repositories hermetic: a developer host
// with core.hooksPath pointing at the workspace hooks would otherwise refuse
// the commits the tests make on main. GIT_CONFIG_* variables take precedence
// over global and repository configuration.
func TestMain(m *testing.M) {
	if os.Getenv("ORBIT_SKIP_HOSTGATE") == "" && os.Getenv("ORBIT_TESTBED") == "" {
		_ = os.Setenv("ORBIT_SKIP_HOSTGATE", "1")
	}
	_ = os.Setenv("GIT_CONFIG_COUNT", "1")
	_ = os.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	_ = os.Setenv("GIT_CONFIG_VALUE_0", "/dev/null")
	os.Exit(m.Run())
}
