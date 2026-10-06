package onboard_test

import (
	"log/slog"
	"os"
	"testing"
)

// Onboarding deliberately writes SSH and user configuration. Test fixtures
// must never use the workstation's ambient home, including failure/retry paths.
func TestMain(m *testing.M) {
	code := runInFixtureHome(m)
	os.Exit(code)
}

func runInFixtureHome(m *testing.M) int {
	home, err := os.MkdirTemp("", "orbit-onboard-tests-")
	if err != nil {
		slog.Error("Create isolated onboarding test home", "error", err)
		return 1
	}
	defer os.RemoveAll(home)
	if err := os.Setenv("HOME", home); err != nil {
		slog.Error("Set isolated onboarding test home", "error", err)
		return 1
	}
	return m.Run()
}
