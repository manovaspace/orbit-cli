package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeGHPRURL = "https://github.com/manovaspace/orbit-test/pull/7"

// fakeGHScript answers `gh` subcommands from files in $FAKE_GH_DIR and appends
// every invocation (one line per call) to $FAKE_GH_DIR/calls.log.
const fakeGHScript = `#!/bin/sh
DIR="$FAKE_GH_DIR"
printf '%s\n' "$*" >> "$DIR/calls.log"
case "$1 $2" in
"pr checks")
	n=0
	if [ -f "$DIR/checks.count" ]; then n=$(cat "$DIR/checks.count"); fi
	n=$((n+1))
	printf '%s' "$n" > "$DIR/checks.count"
	i=$n
	while [ "$i" -gt 1 ] && [ ! -f "$DIR/checks-$i.json" ]; do i=$((i-1)); done
	if [ -f "$DIR/checks-$i.json" ]; then cat "$DIR/checks-$i.json"; fi
	if [ -f "$DIR/checks-$n.stderr" ]; then cat "$DIR/checks-$n.stderr" >&2; fi
	if [ -f "$DIR/checks-$n.exit" ]; then exit "$(cat "$DIR/checks-$n.exit")"; fi
	exit 0
	;;
"pr create")
	echo "__PR_URL__"
	exit 0
	;;
"pr merge")
	if [ -f "$DIR/merge.exit" ]; then exit "$(cat "$DIR/merge.exit")"; fi
	echo "merged"
	exit 0
	;;
"pr comment")
	if [ -f "$DIR/comment.exit" ]; then exit "$(cat "$DIR/comment.exit")"; fi
	echo "commented"
	exit 0
	;;
esac
exit 0
`

// setupFakeGH installs a fake `gh` ahead of the real one on PATH.
func setupFakeGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := strings.ReplaceAll(fakeGHScript, "__PR_URL__", fakeGHPRURL)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake gh: %v", err)
	}
	t.Setenv("FAKE_GH_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func writeGateFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// setChecks installs the answer for the index-th `gh pr checks` call.
func setChecks(t *testing.T, fakeDir string, index int, checksJSON string, exitCode int, stderr string) {
	t.Helper()
	if checksJSON != "" {
		writeGateFile(t, filepath.Join(fakeDir, fmt.Sprintf("checks-%d.json", index)), checksJSON)
	}
	writeGateFile(t, filepath.Join(fakeDir, fmt.Sprintf("checks-%d.exit", index)), fmt.Sprintf("%d", exitCode))
	if stderr != "" {
		writeGateFile(t, filepath.Join(fakeDir, fmt.Sprintf("checks-%d.stderr", index)), stderr)
	}
}

func setGateExit(t *testing.T, fakeDir, name string, code int) {
	t.Helper()
	writeGateFile(t, filepath.Join(fakeDir, name), fmt.Sprintf("%d", code))
}

// gateCalls returns the logged fake `gh` invocations, one per line.
func gateCalls(t *testing.T, fakeDir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fakeDir, "calls.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("reading calls.log: %v", err)
	}
	var calls []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return calls
}

func indexOfCall(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func countCalls(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// shrinkGateTimers keeps the gate polling fast in tests.
func shrinkGateTimers(t *testing.T) {
	t.Helper()
	prevPoll, prevWindow := checksPollInterval, checksRegisterWindow
	checksPollInterval = 5 * time.Millisecond
	checksRegisterWindow = 20 * time.Millisecond
	t.Cleanup(func() {
		checksPollInterval = prevPoll
		checksRegisterWindow = prevWindow
	})
}

func gateGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %s (%v)", strings.Join(args, " "), out, err)
	}
}

// setupGateRepo builds a repository whose origin looks like GitHub but pushes to
// a local bare repository via url.insteadOf.
func setupGateRepo(t *testing.T) string {
	t.Helper()
	localDir, originDir := setupTestGitRepo(t, "manovaspace", "orbit-test")
	gateGit(t, localDir, "remote", "set-url", "origin", "https://github.com/manovaspace/orbit-test.git")
	gateGit(t, localDir, "config", "url."+originDir+".insteadOf", "https://github.com/manovaspace/orbit-test.git")
	shrinkGateTimers(t)
	return localDir
}

// addGateBranch commits a change on a new feature branch.
func addGateBranch(t *testing.T, localDir, branch string) {
	t.Helper()
	gateGit(t, localDir, "checkout", "-b", branch)
	writeGateFile(t, filepath.Join(localDir, "feature.txt"), "feature\n")
	gateGit(t, localDir, "add", "feature.txt")
	gateGit(t, localDir, "commit", "-m", "feat: gate test change")
}

func runGateCmd(t *testing.T, localDir string, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	allArgs := append([]string{}, args...)
	allArgs = append(allArgs, "--dir", localDir)
	cmd.SetArgs(allArgs)
	err := cmd.Execute()
	return buf.String(), err
}

func TestPRMergeAllowsGreen(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"pass"},{"name":"docs","bucket":"skipping"}]`, 0, "")

	if _, err := runGateCmd(t, localDir, "pr", "merge", "7"); err != nil {
		t.Fatalf("expected merge to succeed, got: %v", err)
	}

	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") == -1 {
		t.Errorf("expected a 'pr merge' call, got: %v", calls)
	}
	if indexOfCall(calls, "pr comment") != -1 {
		t.Errorf("expected no 'pr comment' call, got: %v", calls)
	}
}

func TestPRMergeRefusesRed(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"fail"}]`, 1, "")

	_, err := runGateCmd(t, localDir, "pr", "merge", "7")
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
}

func TestPRMergeRefusesPending(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"pending"}]`, 1, "")

	_, err := runGateCmd(t, localDir, "pr", "merge", "7")
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the pending check, got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
	if n := countCalls(calls, "pr checks"); n != 1 {
		t.Errorf("expected exactly one 'pr checks' poll, got %d", n)
	}
}

func TestPRMergeRefusesNoChecks(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, "", 1, "no checks reported on the 'feat/x' branch\n")

	_, err := runGateCmd(t, localDir, "pr", "merge", "7")
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "no checks reported") {
		t.Errorf("expected error to mention 'no checks reported', got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
}

func TestPRMergeAllowNoChecks(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, "", 1, "no checks reported on the 'feat/x' branch\n")

	if _, err := runGateCmd(t, localDir, "pr", "merge", "7", "--allow-no-checks"); err != nil {
		t.Fatalf("expected merge to succeed with --allow-no-checks, got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") == -1 {
		t.Errorf("expected a 'pr merge' call, got: %v", calls)
	}
}

func TestPRMergeRefusesWhenGhFails(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, "garbage output\n", 1, "boom: unexpected failure\n")

	_, err := runGateCmd(t, localDir, "pr", "merge", "7")
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "could not read checks") {
		t.Errorf("expected error to mention 'could not read checks', got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
}

func TestPRMergeOverrideCommentsReason(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"fail"}]`, 1, "")

	if _, err := runGateCmd(t, localDir, "pr", "merge", "7", "--override", "hotfix for outage"); err != nil {
		t.Fatalf("expected override merge to succeed, got: %v", err)
	}

	calls := gateCalls(t, fakeDir)
	commentIdx := indexOfCall(calls, "pr comment")
	mergeIdx := indexOfCall(calls, "pr merge")
	if commentIdx == -1 {
		t.Fatalf("expected a 'pr comment' call, got: %v", calls)
	}
	if mergeIdx == -1 {
		t.Fatalf("expected a 'pr merge' call, got: %v", calls)
	}
	if commentIdx > mergeIdx {
		t.Errorf("expected comment before merge, got: %v", calls)
	}
	if n := countCalls(calls, "pr comment"); n != 1 {
		t.Errorf("expected exactly one comment, got %d", n)
	}
	if !strings.Contains(calls[commentIdx], "override: hotfix for outage") {
		t.Errorf("expected comment body to record the override, got: %s", calls[commentIdx])
	}
}

func TestPRMergeOverrideCommentFailureBlocksMerge(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"fail"}]`, 1, "")
	setGateExit(t, fakeDir, "comment.exit", 1)

	_, err := runGateCmd(t, localDir, "pr", "merge", "7", "--override", "hotfix for outage")
	if err == nil {
		t.Fatal("expected error when override comment fails, got nil")
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
}

func TestPRMergeOverrideBlankRejected(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)

	_, err := runGateCmd(t, localDir, "pr", "merge", "7", "--override", "  ")
	if err == nil {
		t.Fatal("expected error for blank override, got nil")
	}
	if !strings.Contains(err.Error(), "--override requires a non-empty reason") {
		t.Errorf("unexpected error message: %v", err)
	}
	if calls := gateCalls(t, fakeDir); len(calls) != 0 {
		t.Errorf("expected no gh calls, got: %v", calls)
	}
}

func TestAllowNoChecksDoesNotOverrideFailure(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"fail"}]`, 1, "")

	_, err := runGateCmd(t, localDir, "pr", "merge", "7", "--allow-no-checks")
	if err == nil {
		t.Fatal("expected merge to be refused despite --allow-no-checks, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
}

func TestDeliverWaitsThenRefusesRed(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	addGateBranch(t, localDir, "feat/gate-deliver")
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"pending"}]`, 1, "")
	setChecks(t, fakeDir, 2, `[{"name":"ci","bucket":"fail"}]`, 1, "")

	_, err := runGateCmd(t, localDir, "pr", "deliver", "--wait-timeout", "10s")
	if err == nil {
		t.Fatal("expected deliver to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
	if n := countCalls(calls, "pr checks"); n < 2 {
		t.Errorf("expected deliver to poll checks at least twice, got %d", n)
	}
}

func TestDeliverWaitsThenMergesGreen(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	addGateBranch(t, localDir, "feat/gate-green")
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"pending"}]`, 1, "")
	setChecks(t, fakeDir, 2, `[{"name":"ci","bucket":"pass"}]`, 0, "")

	out, err := runGateCmd(t, localDir, "pr", "deliver", "--wait-timeout", "10s")
	if err != nil {
		t.Fatalf("expected deliver to succeed, got: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Successfully delivered feat/gate-green into main!") {
		t.Errorf("expected success message, got: %s", out)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") == -1 {
		t.Errorf("expected a 'pr merge' call, got: %v", calls)
	}
}

func TestDeliverWaitsForLateRegisteringCheck(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	addGateBranch(t, localDir, "feat/gate-late-check")
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"pass"}]`, 0, "")
	setChecks(t, fakeDir, 2, `[{"name":"ci","bucket":"pass"},{"name":"lint","bucket":"fail"}]`, 1, "")
	setChecks(t, fakeDir, 3, `[{"name":"ci","bucket":"pass"},{"name":"lint","bucket":"fail"}]`, 1, "")

	_, err := runGateCmd(t, localDir, "pr", "deliver", "--wait-timeout", "10s")
	if err == nil {
		t.Fatal("expected deliver to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "lint") {
		t.Errorf("expected error to name the late-registering check, got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
	if n := countCalls(calls, "pr checks"); n < 3 {
		t.Errorf("expected deliver to poll checks at least three times, got %d", n)
	}
}

func TestDeliverMergesWhenChecksStable(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	addGateBranch(t, localDir, "feat/gate-stable-checks")
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"pass"}]`, 0, "")
	setChecks(t, fakeDir, 2, `[{"name":"ci","bucket":"pass"},{"name":"lint","bucket":"pass"}]`, 0, "")
	setChecks(t, fakeDir, 3, `[{"name":"ci","bucket":"pass"},{"name":"lint","bucket":"pass"}]`, 0, "")

	out, err := runGateCmd(t, localDir, "pr", "deliver", "--wait-timeout", "10s")
	if err != nil {
		t.Fatalf("expected deliver to succeed, got: %v\nOutput: %s", err, out)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") == -1 {
		t.Errorf("expected a 'pr merge' call, got: %v", calls)
	}
	if n := countCalls(calls, "pr checks"); n < 3 {
		t.Errorf("expected deliver to poll checks at least three times, got %d", n)
	}
}

func TestCreateMergeUsesGate(t *testing.T) {
	fakeDir := setupFakeGH(t)
	localDir := setupGateRepo(t)
	addGateBranch(t, localDir, "feat/gate-create")
	setChecks(t, fakeDir, 1, `[{"name":"ci","bucket":"fail"}]`, 1, "")

	_, err := runGateCmd(t, localDir, "pr", "create", "--merge", "--title", "feat: gate create", "--wait-timeout", "10s")
	if err == nil {
		t.Fatal("expected create --merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	calls := gateCalls(t, fakeDir)
	if indexOfCall(calls, "pr merge") != -1 {
		t.Errorf("expected no 'pr merge' call, got: %v", calls)
	}
	if indexOfCall(calls, "pr create") == -1 {
		t.Errorf("expected a 'pr create' call, got: %v", calls)
	}
}
