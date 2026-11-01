package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/pr"
	"github.com/spf13/cobra"
)

// Gate timings are package-level so tests can shrink them.
var (
	checksPollInterval   = 10 * time.Second
	checksRegisterWindow = 60 * time.Second
)

const defaultGateWaitTimeout = 20 * time.Minute

type gateOptions struct {
	override      string        // --override "reason"
	overrideSet   bool          // true when --override was passed (even if blank)
	allowNoChecks bool          // --allow-no-checks
	wait          bool          // true for deliver and create --merge
	waitTimeout   time.Duration // --wait-timeout, default 20m
}

// addGateFlags registers the merge-gate flags. --wait-timeout is only offered on
// commands that wait for pending checks.
func addGateFlags(cmd *cobra.Command, o *gateOptions, withWait bool) {
	cmd.Flags().StringVar(&o.override, "override", "", "Merge despite failing checks and record the reason as a PR comment")
	cmd.Flags().BoolVar(&o.allowNoChecks, "allow-no-checks", false, "Allow merging when the pull request reports no checks")
	if withWait {
		cmd.Flags().DurationVar(&o.waitTimeout, "wait-timeout", defaultGateWaitTimeout, "Maximum time to wait for pending checks")
	}
}

// runGHSplit runs gh keeping stdout and stderr separate, because JSON on stdout
// must not be mixed with progress or error output on stderr.
func runGHSplit(dir string, args ...string) (stdout, stderr string, err error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", "", errors.New("GitHub CLI ('gh') is required for repositories hosted on GitHub: please install 'gh' and run 'gh auth login'")
	}
	cmd := exec.Command("gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// readChecks fetches the pull request's checks. A nonzero gh exit is not an
// error when stdout parses as a JSON array: gh exits nonzero for failing and
// pending checks too.
func readChecks(dir, ownerRepo, prRef string) ([]pr.Check, error) {
	stdout, stderr, runErr := runGHSplit(dir, "pr", "checks", prRef, "-R", ownerRepo, "--json", "name,bucket")

	checks, parseErr := pr.ParseChecks([]byte(strings.TrimSpace(stdout)))
	if parseErr == nil {
		return checks, nil
	}

	if strings.Contains(stderr, "no checks reported") || strings.Contains(stdout, "no checks reported") {
		return nil, nil
	}

	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = strings.TrimSpace(stdout)
	}
	if detail == "" && runErr != nil {
		detail = runErr.Error()
	}
	if detail == "" {
		detail = parseErr.Error()
	}
	return nil, errors.New(detail)
}

func countPending(checks []pr.Check) int {
	n := 0
	for _, c := range checks {
		if c.Bucket == "pending" {
			n++
		}
	}
	return n
}

// checkNameSet returns the set of check names, ignoring order and duplicates.
func checkNameSet(checks []pr.Check) map[string]struct{} {
	names := make(map[string]struct{}, len(checks))
	for _, c := range checks {
		names[c.Name] = struct{}{}
	}
	return names
}

// sameCheckNameSet reports whether two sets hold exactly the same names.
func sameCheckNameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			return false
		}
	}
	return true
}

// enforceMergeGate refuses to merge unless the pull request's checks pass.
func enforceMergeGate(out io.Writer, dir, ownerRepo, prRef string, o gateOptions) error {
	if o.overrideSet && strings.TrimSpace(o.override) == "" {
		return errors.New("--override requires a non-empty reason")
	}

	waitTimeout := o.waitTimeout
	if waitTimeout <= 0 {
		waitTimeout = defaultGateWaitTimeout
	}

	start := time.Now()
	var last []pr.Check
	var lastNames map[string]struct{}
	polled := false

	for {
		checks, err := readChecks(dir, ownerRepo, prRef)
		if err != nil {
			return fmt.Errorf("merge refused: could not read checks: %w", err)
		}
		last = checks

		if !o.wait {
			break
		}

		names := checkNameSet(last)
		// Workflows register their checks one after another, so a complete poll
		// may still be missing a check that has not registered yet. Accept a
		// complete list only once its names match the previous poll's names.
		complete := countPending(last) == 0 && (len(last) > 0 || time.Since(start) >= checksRegisterWindow)
		stable := polled && sameCheckNameSet(lastNames, names)
		// The first poll is accepted only when the register window has already
		// elapsed and reported no checks at all.
		firstEmpty := !polled && len(last) == 0 && time.Since(start) >= checksRegisterWindow
		if complete && (stable || firstEmpty) {
			break
		}
		lastNames, polled = names, true

		if time.Since(start) >= waitTimeout {
			fmt.Fprintf(out, "%s Check wait timed out after %s\n", iconWarn, waitTimeout)
			break
		}

		fmt.Fprintf(out, "%s Waiting for checks on %s (%d pending, %s elapsed)...\n",
			iconArrow, boldStyle.Render(prRef), countPending(last), time.Since(start).Round(time.Second))
		time.Sleep(checksPollInterval)
	}

	if len(last) == 0 && o.allowNoChecks && (!o.wait || time.Since(start) >= checksRegisterWindow) {
		fmt.Fprintf(out, "%s No checks reported; merging because %s is set\n", iconWarn, boldStyle.Render("--allow-no-checks"))
		return nil
	}

	result := pr.EvaluateChecks(last)
	if result.Allowed {
		return nil
	}

	if reason := strings.TrimSpace(o.override); reason != "" {
		body := "override: " + reason
		if _, err := runGH(dir, "pr", "comment", prRef, "-R", ownerRepo, "--body", body); err != nil {
			return fmt.Errorf("recording override comment: %w", err)
		}
		fmt.Fprintf(out, "%s override recorded: %s\n", iconWarn, reason)
		return nil
	}

	return fmt.Errorf("merge refused: %s (use --override \"reason\" to merge anyway)", result.Reason)
}
