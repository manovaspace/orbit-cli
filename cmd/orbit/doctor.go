package main

import (
	"context"
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	doctorapp "github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/spf13/cobra"
)

// doctorRunner is the seam through which doctor probes external programs.
var doctorRunner runner.Runner = doctor.DefaultRunner()

// doctorNewService builds the diagnostics service; command tests replace it.
var doctorNewService = func(out io.Writer) *doctorapp.Service {
	return doctor.NewService(doctorRunner, out, doctorUI())
}

// doctorWorkspacePaths locates workspace.yaml; command tests replace it.
var doctorWorkspacePaths = func() (root, manifestPath string) {
	root = findWorkspaceRoot("")
	return root, findManifestPath(root, "")
}

// doctorUI maps the CLI styles onto the application UI contract.
func doctorUI() doctorapp.UI {
	return doctorapp.UI{
		OK:        iconOK,
		Warn:      iconWarn,
		Error:     iconError,
		Arrow:     iconArrow,
		Header:    func(text string) string { return headerStyle.Render(text) },
		Bold:      func(text string) string { return boldStyle.Render(text) },
		Success:   func(text string) string { return successStyle.Render(text) },
		Warning:   func(text string) string { return warningStyle.Render(text) },
		ErrorText: func(text string) string { return errorStyle.Render(text) },
		Subtle:    func(text string) string { return subtleStyle.Render(text) },
	}
}

func newDoctorCmd() *cobra.Command {
	var (
		jsonOutput                    bool
		fix                           bool
		nonInteractive                bool
		yesFlag                       bool
		local, remote, acceptHostKeys bool
	)

	cmd := &cobra.Command{
		Use:          "doctor",
		Short:        "Run pre-flight system diagnostics and environment health checks",
		Long:         "Local diagnostics are the default: no remote probes, updater activity or repairs. --remote selects SSH, Docker daemon, listener and cloud probes. --fix explicitly selects repairs; --accept-host-keys additionally requires --remote --fix. --local rejects remote probes and repairs.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if local && (remote || fix || acceptHostKeys) {
				err := clierr.Usage("--local cannot be combined with remote probes or mutation flags")
				if jsonOutput {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			if acceptHostKeys && (!remote || !fix) {
				err := clierr.Usage("--accept-host-keys requires --remote --fix")
				if jsonOutput {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}

			uiOut := io.Writer(out)
			if jsonOutput {
				uiOut = io.Discard
			}

			workspaceRoot, manifestPath := doctorWorkspacePaths()
			opts := doctorapp.Options{
				Remote:         remote,
				AcceptHostKeys: acceptHostKeys,
				Fix:            fix,
				JSON:           jsonOutput,
				WorkspaceRoot:  workspaceRoot,
				ManifestPath:   manifestPath,
			}

			service := doctorNewService(uiOut)
			service.Extra = func(ctx context.Context, report *doctor.DoctorReport, remote, fix bool) {
				addSelectedAssetDiagnostics(ctx, report, remote, fix)
			}

			result := service.Diagnose(cmd.Context(), opts)

			if !jsonOutput {
				fmt.Fprintln(out, titleStyle.Render("Orbit System Doctor — Pre-flight Diagnostics"))
				renderDoctorReport(out, result.Report)
			}

			if fix {
				heal, err := service.Heal(cmd.Context(), result.Report, opts)
				if err != nil {
					if jsonOutput {
						_ = output.Write(out, output.Failure(err))
					}
					return err
				}
				if heal.Ran {
					result.Report = heal.Report
					if !jsonOutput {
						fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Post-Healing Diagnostic Report ─────────────────────────"))
						renderDoctorReport(out, result.Report)
					}
				}
			}

			if result.Report.HasErrors() {
				err := clierr.Failed(fmt.Sprintf("pre-flight diagnostics failed with %d error(s)", result.Report.CountErrors()))
				if jsonOutput {
					// Keep the report in data, as the pre-envelope --json output
					// always carried it, while still reporting failure.
					envelope := output.Failure(err)
					envelope.Data = result.Report
					_ = output.Write(out, envelope)
				}
				return err
			}

			if jsonOutput {
				return output.Write(out, output.Success(result.Report))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "Explicitly require diagnostics without remote probes or writes (default behavior)")
	cmd.Flags().BoolVar(&remote, "remote", false, "Opt in to remote SSH/cloud and daemon/listener probes")
	cmd.Flags().BoolVar(&acceptHostKeys, "accept-host-keys", false, "Accept new SSH host keys (requires --remote --fix)")
	cmd.Flags().BoolVarP(&fix, "fix", "f", false, "Automatically install and configure missing toolchain dependencies")
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Compatibility flag; doctor does not prompt")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Compatibility flag; doctor does not prompt")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print the diagnostic report as a JSON envelope")

	return cmd
}

func renderDoctorReport(out io.Writer, report *doctor.DoctorReport) (passed, warnings, errors int) {
	for _, res := range report.Results {
		switch res.Status {
		case doctor.StatusOK:
			passed++
		case doctor.StatusWarning, doctor.StatusUnverified:
			warnings++
		case doctor.StatusError:
			errors++
		}
	}

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
		case doctor.StatusWarning, doctor.StatusUnverified:
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

	summary := fmt.Sprintf("\n%s  %s  %s",
		successStyle.Render(fmt.Sprintf("✔ %d passed", passed)),
		warningStyle.Render(fmt.Sprintf("⚠ %d warnings", warnings)),
		errorStyle.Render(fmt.Sprintf("✖ %d errors", errors)),
	)
	fmt.Fprintln(out, summary)

	return passed, warnings, errors
}
