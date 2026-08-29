package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/doctor/healer"
	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var (
		jsonOutput     bool
		fix            bool
		nonInteractive bool
		yesFlag        bool
	)

	cmd := &cobra.Command{
		Use:          "doctor",
		Short:        "Run pre-flight system diagnostics and environment health checks",
		Long:         "Executes comprehensive diagnostics across OS, Go compiler, Node/Bun, Docker, SSH keys, dev ports, and optional tools.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			ctx := cmd.Context()

			report := doctor.RunDiagnostics()
			addAssetDiagnostics(cmd.Context(), report, fix)

			if jsonOutput {
				if fix {
					reg := healer.NewDefaultRegistry()
					healables := reg.FindHealers(report.Results)
					if len(healables) > 0 {
						_, _ = reg.Run(ctx, report.Results, nil)
						report = doctor.RunDiagnostics()
						addAssetDiagnostics(ctx, report, true)
					}
				}

				data, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal diagnostic report to JSON: %w", err)
				}
				fmt.Fprintln(out, string(data))

				if report.HasErrors() {
					return fmt.Errorf("pre-flight diagnostics failed with %d error(s)", countErrors(report))
				}

				return nil
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit System Doctor — Pre-flight Diagnostics"))

			renderDoctorReport(out, report)

			reg := healer.NewDefaultRegistry()
			healableHealers := reg.FindHealers(report.Results)

			isNonInteractive := nonInteractive || yesFlag
			shouldHeal := fix
			if !shouldHeal && len(healableHealers) > 0 && !isNonInteractive && (istty.IsInteractiveSession() || cmd.InOrStdin() != os.Stdin) {
				fmt.Fprintln(out)
				promptMsg := fmt.Sprintf("Found %d auto-healable toolchain issue(s). Attempt automated fix?", len(healableHealers))
				shouldHeal = promptYesNo(in, out, promptMsg, true)
			}

			if shouldHeal && len(healableHealers) > 0 {
				fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Auto-Healing Toolchains & Dependencies ─────────────────"))
				_, _ = healer.RunHealers(ctx, report.Results, func(name, status string) {
					if status == "Completed successfully" {
						fmt.Fprintf(out, "  %s  %-24s %s\n", iconOK, boldStyle.Render(name), successStyle.Render("Installed and configured successfully"))
					} else if strings.HasPrefix(status, "Failed:") {
						fmt.Fprintf(out, "  %s  %-24s %s\n", iconError, boldStyle.Render(name), errorStyle.Render(status))
					} else {
						fmt.Fprintf(out, "  %s  %-24s %s\n", iconArrow, boldStyle.Render(name), subtleStyle.Render(status))
					}
				})

				// Re-evaluate diagnostics after auto-healing
				report = doctor.RunDiagnostics()
				addAssetDiagnostics(ctx, report, false)
				fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Post-Healing Diagnostic Report ─────────────────────────"))
				renderDoctorReport(out, report)
			}

			if report.HasErrors() {
				return fmt.Errorf("pre-flight diagnostics failed with %d error(s)", countErrors(report))
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&fix, "fix", "f", false, "Automatically install and configure missing toolchain dependencies")
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Skip interactive confirmation prompts")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable interactive prompts")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output diagnostic report in JSON format")

	return cmd
}

func countErrors(report *doctor.DoctorReport) int {
	if report == nil {
		return 0
	}
	count := 0
	for _, res := range report.Results {
		if res.Status == doctor.StatusError {
			count++
		}
	}
	return count
}

func renderDoctorReport(out io.Writer, report *doctor.DoctorReport) (passed, warnings, errors int) {
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

	// Render results grouped by category
	currentCategory := ""
	var fixes []doctor.DiagnosticResult

	for _, res := range report.Results {
		if res.Category != currentCategory {
			currentCategory = res.Category
			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── "+currentCategory+" ──────────────────────────────────"))
		}

		var statusIcon string
		switch res.Status {
		case doctor.StatusOK:
			statusIcon = iconOK
		case doctor.StatusWarning:
			statusIcon = iconWarn
		case doctor.StatusError:
			statusIcon = iconError
		default:
			statusIcon = iconInfo
		}

		nameCol := padRight(res.Name, 26)
		fmt.Fprintf(out, "  %s  %s  %s\n", statusIcon, nameCol, res.Message)

		if res.FixSuggestion != "" && res.Status != doctor.StatusOK {
			fixes = append(fixes, res)
		}
	}

	// Render remediation section if any warnings/errors have suggestions
	if len(fixes) > 0 {
		fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Remediation & Fix Suggestions ──────────────────────────"))
		for _, fix := range fixes {
			var badge string
			if fix.Status == doctor.StatusError {
				badge = errorStyle.Render("[ERROR]")
			} else {
				badge = warningStyle.Render("[WARN]")
			}
			fmt.Fprintf(out, "  %s %s: %s\n     %s %s\n",
				badge,
				boldStyle.Render(fix.Name),
				fix.Message,
				iconArrow,
				codeStyle.Render(fix.FixSuggestion),
			)
		}
	}

	// Summary footer
	summary := fmt.Sprintf("\n%s  %s  %s",
		successStyle.Render(fmt.Sprintf("✔ %d passed", passed)),
		warningStyle.Render(fmt.Sprintf("⚠ %d warnings", warnings)),
		errorStyle.Render(fmt.Sprintf("✖ %d errors", errors)),
	)
	fmt.Fprintln(out, summary)

	return passed, warnings, errors
}
