package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	doctorapp "github.com/manovaspace/orbit-cli/pkg/doctor/application"
	doctordomain "github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
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
		OK:        render.IconOK,
		Warn:      render.IconWarn,
		Error:     render.IconError,
		Arrow:     render.IconArrow,
		Header:    func(text string) string { return render.Header.Render(text) },
		Bold:      func(text string) string { return render.Bold.Render(text) },
		Success:   func(text string) string { return render.Success.Render(text) },
		Warning:   func(text string) string { return render.Warning.Render(text) },
		ErrorText: func(text string) string { return render.Error.Render(text) },
		Subtle:    func(text string) string { return render.Subtle.Render(text) },
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

			workspaceRoot, manifestPath := doctorWorkspacePaths()
			opts := doctorapp.Options{
				Remote:         remote,
				AcceptHostKeys: acceptHostKeys,
				Fix:            fix,
				JSON:           jsonOutput,
				WorkspaceRoot:  workspaceRoot,
				ManifestPath:   manifestPath,
			}

			service := doctorNewService(io.Discard)
			service.Extra = func(ctx context.Context, report *doctor.DoctorReport, remote, fix bool) {
				addSelectedAssetDiagnostics(ctx, report, remote, fix)
			}

			result := service.Diagnose(cmd.Context(), opts)
			var heals []doctordomain.HealResult

			if fix {
				heal, err := service.Heal(cmd.Context(), result.Report, opts)
				if err != nil {
					if jsonOutput {
						_ = output.Write(out, output.Failure(err))
						return err
					}
					p := render.NewPrinter(out)
					renderDoctorText(p, result.Report, nil)
					if perr := p.Err(); perr != nil {
						return perr
					}
					return err
				}
				if heal.Ran {
					result.Report = heal.Report
					heals = heal.HealResults
				}
			}

			text := func(p *render.Printer) error {
				renderDoctorText(p, result.Report, heals)
				return nil
			}

			if result.Report.HasErrors() {
				err := clierr.Failed(fmt.Sprintf("pre-flight diagnostics failed with %d error(s)", result.Report.CountErrors()))
				// Keep the report in data, as the pre-envelope --json output
				// always carried it, while still reporting failure.
				return render.EmitFindings(out, jsonOutput, result.Report, err, text)
			}

			return render.Emit(out, jsonOutput, result.Report, text)
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

// renderDoctorText writes the diagnostic report as category sections, optional
// healing results, fix suggestions and one summary line.
func renderDoctorText(p *render.Printer, report *doctor.DoctorReport, heals []doctordomain.HealResult) {
	p.Title("System doctor")

	if len(heals) > 0 {
		p.Section("Auto-healing")
		for _, heal := range heals {
			status := render.StatusOK
			if !heal.Success {
				status = render.StatusFail
			}
			p.Line(status, fmt.Sprintf("%s: %s", heal.HealerName, heal.Message))
		}
	}

	if report == nil || len(report.Results) == 0 {
		p.Empty(render.Plural(0, "diagnostic", "diagnostics"))
	} else {
		currentCategory := ""
		var fixes []doctor.DiagnosticResult
		for _, res := range report.Results {
			if res.Category != currentCategory {
				currentCategory = res.Category
				p.Section(currentCategory)
			}
			text := strings.TrimRight(fmt.Sprintf("%-26s  %s", res.Name, res.Message), " ")
			p.Line(doctorTextStatus(res.Status), text)

			if res.FixSuggestion != "" && res.Status != doctor.StatusOK {
				fixes = append(fixes, res)
			}
		}

		if len(fixes) > 0 {
			p.Section("Remediation & fix suggestions")
			for _, fix := range fixes {
				p.Line(doctorTextStatus(fix.Status), fmt.Sprintf("%s: %s", fix.Name, fix.Message))
				p.Line(render.StatusInfo, fix.FixSuggestion)
			}
		}
	}

	passed, warnings, unverified, errors := doctorCounts(report)
	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: passed, Label: "passed"},
		render.SummaryPart{Status: render.StatusWarn, Count: warnings, Label: render.Plural(warnings, "warning", "warnings")},
		render.SummaryPart{Status: render.StatusUnverified, Count: unverified, Label: "unverified"},
		render.SummaryPart{Status: render.StatusFail, Count: errors, Label: render.Plural(errors, "error", "errors")},
	)
}

func doctorCounts(report *doctor.DoctorReport) (passed, warnings, unverified, errors int) {
	if report == nil {
		return 0, 0, 0, 0
	}
	for _, res := range report.Results {
		switch res.Status {
		case doctor.StatusOK:
			passed++
		case doctor.StatusWarning:
			warnings++
		case doctor.StatusUnverified:
			unverified++
		case doctor.StatusError:
			errors++
		}
	}
	return passed, warnings, unverified, errors
}

func doctorTextStatus(status doctor.CheckStatus) render.Status {
	switch status {
	case doctor.StatusOK:
		return render.StatusOK
	case doctor.StatusWarning:
		return render.StatusWarn
	case doctor.StatusUnverified:
		return render.StatusUnverified
	case doctor.StatusError:
		return render.StatusFail
	default:
		return render.StatusInfo
	}
}
