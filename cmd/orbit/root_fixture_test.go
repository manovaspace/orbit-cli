package main

import (
	"github.com/spf13/cobra"
	"testing"
)

// Command fixtures exercise behavior independently of the machine's login shell
// and PATH. Host eligibility is verified separately by hostgate_test.go.
func newRootCmdForTest(t *testing.T) *cobra.Command {
	t.Helper()
	t.Setenv("ORBIT_SKIP_HOSTGATE", "1")
	return newRootCmd()
}
