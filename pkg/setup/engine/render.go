package engine

import "github.com/manovaspace/orbit-cli/pkg/render"

// RenderPlan writes one section per part: its status, findings, errors and planned actions.
func RenderPlan(p *render.Printer, r Result) {
	renderParts(p, r)
}

// RenderResult writes the same sections as RenderPlan, then a summary of changed and failed parts.
func RenderResult(p *render.Printer, r Result) {
	renderParts(p, r)
	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: r.Changed, Label: "changed"},
		render.SummaryPart{Status: render.StatusFail, Count: r.Failed, Label: "failed"},
	)
}

func renderParts(p *render.Printer, r Result) {
	for _, pr := range r.Parts {
		p.Section(pr.Part)
		p.Line(statusIcon(pr.Status), pr.Status)
		for _, f := range pr.Findings {
			p.Line(severityIcon(f.Severity), f.Message)
			if f.Fix != "" {
				p.Hint(f.Fix)
			}
		}
		if pr.Error != "" {
			p.Line(render.StatusFail, pr.Error)
		}
		for _, action := range pr.Actions {
			p.Line(render.StatusInfo, action)
		}
	}
}

func statusIcon(status string) render.Status {
	switch status {
	case StatusOK, StatusChanged:
		return render.StatusOK
	case StatusPending:
		return render.StatusInfo
	case StatusFailed:
		return render.StatusFail
	case StatusSkipped:
		return render.StatusSkipped
	case StatusWaits:
		return render.StatusUnverified
	default:
		return render.StatusInfo
	}
}

func severityIcon(sev Severity) render.Status {
	if sev == SeverityError {
		return render.StatusFail
	}
	return render.StatusWarn
}
