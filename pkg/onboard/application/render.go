package application

import (
	"fmt"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
)

// Step headings keep the exact widths the command has always printed.
const (
	headerDiagnostics = "── Step 1: Pre-Flight System Diagnostics ──────────────────"
	headerHealing     = "── Auto-Healing Toolchains & Dependencies ─────────────────"
	headerPostHealing = "── Post-Healing Diagnostic Report ─────────────────────────"
	headerKeypair     = "── Step 2: SSH Developer Keypair ──────────────────────────"
	headerClaim       = "── Step 3: Identity & Provisioning Claim ──────────────────"
	headerNetwork     = "── Step 4: Network & Local TLS Setup ───────────────────────"
	headerWorkspace   = "── Step 5: Workspace & Repositories ───────────────────────"
	headerMCP         = "── Step 6: Cursor MCP & IDE Integration ───────────────────"
	headerStack       = "── Step 7: Local Development Stack ────────────────────────"
)

// withDefaults fills the decoration the CLI always supplies so a Service built
// without a UI still renders plain text instead of panicking.
func (u UI) withDefaults() UI {
	identity := func(text string) string { return text }
	if u.Title == nil {
		u.Title = identity
	}
	if u.Header == nil {
		u.Header = identity
	}
	if u.Bold == nil {
		u.Bold = identity
	}
	if u.Success == nil {
		u.Success = identity
	}
	if u.Warning == nil {
		u.Warning = identity
	}
	if u.ErrorText == nil {
		u.ErrorText = identity
	}
	if u.InfoText == nil {
		u.InfoText = identity
	}
	if u.Subtle == nil {
		u.Subtle = identity
	}
	if u.Code == nil {
		u.Code = identity
	}
	if u.Card == nil {
		u.Card = func(title, content string) string {
			if title == "" {
				return content
			}
			return title + "\n\n" + content
		}
	}
	return u
}

func countDiagnosticResults(report *doctor.DoctorReport) (passed, warnings, errors int) {
	if report == nil {
		return 0, 0, 0
	}
	for _, res := range report.Results {
		switch res.Status {
		case doctor.StatusOK:
			passed++
		case doctor.StatusWarning:
			warnings++
		case doctor.StatusError:
			errors++
		}
	}
	return passed, warnings, errors
}

func collectFixSuggestions(report *doctor.DoctorReport) []doctor.DiagnosticResult {
	if report == nil {
		return nil
	}
	var fixes []doctor.DiagnosticResult
	for _, res := range report.Results {
		if res.FixSuggestion != "" && res.Status != doctor.StatusOK {
			fixes = append(fixes, res)
		}
	}
	return fixes
}

func (s *Service) renderDiagnosticResults(report *doctor.DoctorReport, fixes []doctor.DiagnosticResult, passed, warnings, errorsCount int) {
	if report == nil {
		return
	}
	for _, res := range report.Results {
		var icon string
		switch res.Status {
		case doctor.StatusOK:
			icon = s.UI.OK
		case doctor.StatusWarning:
			icon = s.UI.Warn
		case doctor.StatusError:
			icon = s.UI.Error
		default:
			icon = s.UI.Info
		}
		fmt.Fprintf(s.out(), "  %s  %-26s %s\n", icon, res.Name, res.Message)
	}

	if len(fixes) > 0 {
		fmt.Fprintf(s.out(), "\n%s\n", s.UI.Header("── Diagnostic Fix Suggestions ─────────────────────────────"))
		for _, fix := range fixes {
			var badge string
			if fix.Status == doctor.StatusError {
				badge = s.UI.ErrorText("[ERROR]")
			} else {
				badge = s.UI.Warning("[WARN]")
			}
			fmt.Fprintf(s.out(), "  %s %s: %s\n     %s %s\n",
				badge,
				s.UI.Bold(fix.Name),
				fix.Message,
				s.UI.Arrow,
				s.UI.Code(fix.FixSuggestion),
			)
		}
	}

	fmt.Fprintf(s.out(), "\n  %s  %s  %s\n",
		s.UI.Success(fmt.Sprintf("✔ %d passed", passed)),
		s.UI.Warning(fmt.Sprintf("⚠ %d warnings", warnings)),
		s.UI.ErrorText(fmt.Sprintf("✖ %d errors", errorsCount)),
	)
}

// renderSummary prints the completion card with the developer's details and the
// next steps.
func (s *Service) renderSummary(workspaceRoot string, sess *domain.SessionState) {
	var extraDetails string
	totpVal := ""
	if sess.Metadata != nil {
		for _, k := range []string{"totp_uri", "otpauth", "totp_secret", "totp"} {
			if v, ok := sess.Metadata[k]; ok && v != "" {
				totpVal = v
				break
			}
		}
	}
	if totpVal != "" {
		extraDetails = fmt.Sprintf("\nAuthelia 2FA:   %s\n                %s",
			s.UI.InfoText("TOTP Enrollment Ready"),
			s.UI.Subtle(totpVal),
		)
	}

	summaryText := fmt.Sprintf(
		"Developer ID:   %s\n"+
			"Email:          %s\n"+
			"Workspace:      %s\n"+
			"Cursor MCP:     %s\n"+
			"VPN Profile:    %s%s\n\n"+
			"Next Steps:\n"+
			"  1. Launch Cursor in this workspace:\n"+
			"     %s\n"+
			"  2. Check development environment status:\n"+
			"     %s\n"+
			"  3. Open Developer Portal:\n"+
			"     %s",
		s.UI.Bold(sess.UID),
		s.UI.Subtle(sess.Email),
		s.UI.Subtle(workspaceRoot),
		s.UI.Success(".cursor/mcp.env (configured)"),
		s.UI.InfoText("~/.config/orbit/wg0.conf"),
		extraDetails,
		s.UI.Code("cursor "+workspaceRoot),
		s.UI.Code("orbit status"),
		s.UI.Code("orbit dev portal"),
	)
	fmt.Fprintln(s.out(), s.UI.Card("ONBOARDING COMPLETE", summaryText))
}
