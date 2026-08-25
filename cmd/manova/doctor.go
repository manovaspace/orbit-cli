package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var (
		jsonOutput bool
		fixOutput  bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run pre-flight system diagnostics and environment health checks",
		Long:  "Executes comprehensive diagnostics across OS, Go compiler, Node/pnpm, Docker, SSH keys, dev ports, and optional tools.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Manova System Doctor — Pre-flight Diagnostics"))

			report := doctor.RunDiagnostics()

			// Count outcomes
			passed := 0
			warnings := 0
			errors := 0

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

			if fixOutput {
				execPath, _ := os.Executable()
				postCtx := &migrate.PostUpdateContext{
					Interactive: true,
					In:          cmd.InOrStdin(),
					Out:         out,
					ExecPath:    execPath,
				}
				if migResults, err := migrate.RunPostUpdateMigrations(postCtx); err == nil {
					for _, r := range migResults {
						if r.Success && !r.Skipped {
							fmt.Fprintf(out, "  %s  %s\n", iconOK, subtleStyle.Render(r.Description))
						}
					}
				}
			}

			if report.HasErrors() {
				if fixOutput {
					fmt.Fprintf(out, "\n  %s  %s\n\n", iconInfo, infoStyle.Render("Auto-installing missing dependencies..."))
					_ = doctor.AutoInstallDependencies(cmd.Context(), report, out)
					// Re-evaluate
					report = doctor.RunDiagnostics()
					if !report.HasErrors() {
						fmt.Fprintf(out, "\n  %s  %s\n", iconOK, successStyle.Render("All dependencies installed and verified successfully!"))
						return nil
					}
				}
				if !fixOutput {
					fmt.Fprintf(out, "\n  %s  %s\n", iconWarn, warningStyle.Render("Run 'manova doctor --fix' to automatically install missing dependencies."))
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output diagnostic report in JSON format")
	cmd.Flags().BoolVar(&fixOutput, "fix", false, "Automatically install missing dependencies and tools")
	cmd.Flags().BoolVar(&fixOutput, "auto-install-deps", false, "Alias for --fix")

	return cmd
}
