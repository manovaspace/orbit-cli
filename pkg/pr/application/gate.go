package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

// enforceGate refuses to merge unless the pull request's checks pass.
func (s *Service) enforceGate(ctx context.Context, repo domain.Repo, ref string, o domain.MergeOptions) error {
	if o.OverrideSet && strings.TrimSpace(o.Override) == "" {
		return clierr.Usage("--override requires a non-empty reason")
	}

	waitTimeout := o.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = DefaultWaitTimeout
	}
	pollInterval := s.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	registerWindow := s.RegisterWindow
	if registerWindow <= 0 {
		registerWindow = DefaultRegisterWindow
	}

	start := s.now()
	var last []domain.Check
	var lastNames map[string]struct{}
	polled := false

	for {
		checks, err := s.Forge.Checks(ctx, repo, ref)
		if err != nil {
			return withMessage(err, "merge refused: could not read checks")
		}
		last = checks

		if !o.Wait {
			break
		}

		names := checkNameSet(last)
		// Workflows register their checks one after another, so a complete poll
		// may still be missing a check that has not registered yet. Accept a
		// complete list only once its names match the previous poll's names.
		elapsed := s.now().Sub(start)
		complete := countPending(last) == 0 && (len(last) > 0 || elapsed >= registerWindow)
		stable := polled && sameCheckNameSet(lastNames, names)
		// The first poll is accepted only when the register window has already
		// elapsed and reported no checks at all.
		firstEmpty := !polled && len(last) == 0 && elapsed >= registerWindow
		if complete && (stable || firstEmpty) {
			break
		}
		lastNames, polled = names, true

		if elapsed >= waitTimeout {
			fmt.Fprintf(s.out(), "%s Check wait timed out after %s\n", s.icon(s.UI.Warn), waitTimeout)
			break
		}

		fmt.Fprintf(s.out(), "%s Waiting for checks on %s (%d pending, %s elapsed)...\n",
			s.icon(s.UI.Arrow), s.bold(ref), countPending(last), elapsed.Round(time.Second))
		s.sleep(pollInterval)
	}

	if len(last) == 0 && o.AllowNoChecks && (!o.Wait || s.now().Sub(start) >= registerWindow) {
		fmt.Fprintf(s.out(), "%s No checks reported; merging because %s is set\n", s.icon(s.UI.Warn), s.bold("--allow-no-checks"))
		return nil
	}

	result := domain.EvaluateChecks(last)
	if result.Allowed {
		return nil
	}

	if reason := strings.TrimSpace(o.Override); reason != "" {
		body := "override: " + reason
		if err := s.Forge.Comment(ctx, repo, ref, body); err != nil {
			return withMessage(err, "recording override comment")
		}
		fmt.Fprintf(s.out(), "%s override recorded: %s\n", s.icon(s.UI.Warn), reason)
		return nil
	}

	return clierr.Failed(fmt.Sprintf("merge refused: %s (use --override \"reason\" to merge anyway)", result.Reason))
}

func countPending(checks []domain.Check) int {
	n := 0
	for _, c := range checks {
		if c.Bucket == "pending" {
			n++
		}
	}
	return n
}

// checkNameSet returns the set of check names, ignoring order and duplicates.
func checkNameSet(checks []domain.Check) map[string]struct{} {
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
