package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// Check plans every part in order and applies nothing. A part whose Needs failed or were
// skipped is skipped without planning. A part whose Plan errors is failed, unless one of its
// Needs is still pending (or waiting), in which case it is waits: the error is kept but not
// counted, and the part is planned again by Apply after the need has run.
func (e *Engine) Check(ctx context.Context, s *State) Result {
	var res Result
	status := make(map[string]string, len(e.Parts))
	for _, part := range e.Parts {
		pr, _ := planPart(ctx, s, part, status)
		if pr.Status == StatusPending {
			res.Changed++
		}
		if pr.Status == StatusFailed {
			res.Failed++
		}
		status[pr.Part] = pr.Status
		res.Parts = append(res.Parts, pr)
	}
	return res
}

// Apply previews with Check, asks Confirm once when the preview has actions, then runs each
// part in order: reload the state, plan again, and run the actions. The first failing action
// fails its part and stops that part's remaining actions. A part whose Needs failed or were
// skipped is skipped.
func (e *Engine) Apply(ctx context.Context, s *State) (Result, error) {
	preview := e.Check(ctx, s)
	if preview.Changed > 0 && e.Confirm != nil && !e.Confirm(preview.Parts) {
		return preview, clierr.Usage("setup cancelled")
	}

	var res Result
	status := make(map[string]string, len(e.Parts))
	for _, part := range e.Parts {
		var pr PartResult
		if blocked(part.Needs(), status) {
			pr = PartResult{Part: part.Name(), Status: StatusSkipped}
		} else if err := s.Reload(); err != nil {
			pr = PartResult{Part: part.Name(), Status: StatusFailed, Error: err.Error()}
		} else {
			var plan Plan
			pr, plan = planPart(ctx, s, part, status)
			if pr.Status == StatusPending {
				pr = runActions(ctx, pr, plan)
			}
		}
		switch pr.Status {
		case StatusChanged:
			res.Changed++
		case StatusFailed:
			res.Failed++
		}
		status[pr.Part] = pr.Status
		res.Parts = append(res.Parts, pr)
	}
	if res.Failed > 0 {
		return res, clierr.Failed(fmt.Sprintf("setup incomplete: %d parts failed", res.Failed))
	}
	return res, nil
}

// planPart plans one part given the statuses of the parts that ran before it. It returns the
// plan only when the part was planned without error. Pending is set when the plan has actions;
// Failed when the plan errors or has an error finding.
func planPart(ctx context.Context, s *State, part Part, status map[string]string) (PartResult, Plan) {
	pr := PartResult{Part: part.Name()}
	needs := part.Needs()
	if blocked(needs, status) {
		pr.Status = StatusSkipped
		return pr, Plan{}
	}

	plan, err := part.Plan(ctx, s)
	if err != nil {
		pr.Error = err.Error()
		if waiting(needs, status) {
			pr.Status = StatusWaits
		} else {
			pr.Status = StatusFailed
		}
		return pr, Plan{}
	}

	pr.Findings = plan.Findings
	for _, action := range plan.Actions {
		pr.Actions = append(pr.Actions, actionLabel(action))
	}
	pr.Status = StatusOK
	if len(plan.Actions) > 0 {
		pr.Status = StatusPending
	}
	for _, f := range plan.Findings {
		if f.Severity == SeverityError {
			pr.Status = StatusFailed
			return pr, Plan{}
		}
	}
	return pr, plan
}

// runActions runs the actions of a planned part in order. The first failure fails the part.
func runActions(ctx context.Context, pr PartResult, plan Plan) PartResult {
	for _, action := range plan.Actions {
		if action.Do == nil {
			pr.Status = StatusFailed
			pr.Error = fmt.Sprintf("action %q has no run function", action.Summary)
			return pr
		}
		if err := action.Do(ctx); err != nil {
			pr.Status = StatusFailed
			pr.Error = err.Error()
			return pr
		}
	}
	pr.Status = StatusChanged
	return pr
}

// blocked reports whether any need failed or was skipped.
func blocked(needs []string, status map[string]string) bool {
	for _, need := range needs {
		if st := status[need]; st == StatusFailed || st == StatusSkipped {
			return true
		}
	}
	return false
}

// waiting reports whether any need still has actions pending or is itself waiting.
func waiting(needs []string, status map[string]string) bool {
	for _, need := range needs {
		if st := status[need]; st == StatusPending || st == StatusWaits {
			return true
		}
	}
	return false
}

// actionLabel is the summary with its [sudo] and [browser] markers.
func actionLabel(a Action) string {
	var b strings.Builder
	if a.Sudo {
		b.WriteString("[sudo] ")
	}
	if a.Browser {
		b.WriteString("[browser] ")
	}
	b.WriteString(a.Summary)
	return b.String()
}
