package main

import (
	"bytes"
	"testing"
)

// runRootForTest executes the whole command tree so persistent flags and the
// host-gate skip apply exactly as in production.
func runRootForTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cmd := newRootCmdForTest(t)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}
