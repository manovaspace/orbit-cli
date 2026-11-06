package main

import (
	"bytes"
	"regexp"
	"testing"
)

// ansiEscape matches CSI sequences emitted by lipgloss v2, which always renders
// full-fidelity ANSI regardless of the destination writer.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// stripANSI removes styling so assertions can match the visible text.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

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
