package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/doctor/healer"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/spf13/cobra"
)

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
				return fmt.Errorf("--local cannot be combined with remote probes or mutation flags")
			}
			if acceptHostKeys && !(remote && fix) {
				return fmt.Errorf("--accept-host-keys requires --remote --fix")
			}
			opts := doctor.DiagnosticOptions{Remote: remote, AcceptHostKeys: acceptHostKeys}
			ctx := cmd.Context()

			report := doctor.RunDiagnosticsWithOptions(opts)
			addSelectedAssetDiagnostics(cmd.Context(), report, remote, fix)
			addWorkspaceGitDiagnostics(report)

			if jsonOutput {
				if fix {
					reg := healer.NewDefaultRegistry()
					healables := reg.FindHealers(report.Results)
					if len(healables) > 0 {
						_, _ = reg.Run(ctx, report.Results, nil)
						report = doctor.RunDiagnosticsWithOptions(opts)
						addSelectedAssetDiagnostics(ctx, report, remote, true)
						addWorkspaceGitDiagnostics(report)
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

			shouldHeal := fix

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
				report = doctor.RunDiagnosticsWithOptions(opts)
				addSelectedAssetDiagnostics(ctx, report, remote, false)
				addWorkspaceGitDiagnostics(report)
				fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Post-Healing Diagnostic Report ─────────────────────────"))
				renderDoctorReport(out, report)
			}

			if report.HasErrors() {
				return fmt.Errorf("pre-flight diagnostics failed with %d error(s)", countErrors(report))
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
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output diagnostic report in JSON format")

	return cmd
}

func addWorkspaceGitDiagnostics(report *doctor.DoctorReport) {
	if report == nil {
		return
	}
	root := findWorkspaceRoot("")
	manifestPath := findManifestPath(root, "")
	if _, err := os.Stat(manifestPath); err != nil {
		report.Add(doctor.DiagnosticResult{Category: "Workspace", Name: "Manifest", Status: doctor.StatusError,
			Message: "Workspace targets unverified: cannot inspect workspace.yaml", FixSuggestion: "Provide a readable workspace.yaml for workspace diagnostics."})
		return
	}
	m, err := manifest.Load(manifestPath)
	if err != nil {
		report.Add(doctor.DiagnosticResult{
			Category:      "Workspace",
			Name:          "Manifest",
			Status:        doctor.StatusError,
			Message:       "Workspace targets unverified: cannot load workspace.yaml",
			FixSuggestion: "Fix workspace.yaml readability or parse errors.",
		})
		return
	}
	targets := m.ResolveScope("all")
	if len(targets) == 0 {
		report.Add(doctor.DiagnosticResult{Category: "Workspace", Name: "Manifest", Status: doctor.StatusError,
			Message: "Workspace targets unverified: workspace.yaml declares no repository targets", FixSuggestion: "Declare the intended repositories before workspace diagnostics."})
		return
	}
	gitless, missing, inspectionErrors := 0, 0, 0
	for _, target := range targets {
		repoPath := filepath.Join(root, target.Path)
		info, err := os.Stat(repoPath)
		if os.IsNotExist(err) {
			missing++
			continue
		}
		if err != nil || !info.IsDir() {
			inspectionErrors++
			report.Add(doctor.DiagnosticResult{Category: "Workspace", Name: target.Name, Status: doctor.StatusError, Message: "Cannot inspect repository directory"})
			continue
		}
		if _, err := os.Lstat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
			gitless++
		} else if err != nil {
			inspectionErrors++
			report.Add(doctor.DiagnosticResult{Category: "Workspace", Name: target.Name, Status: doctor.StatusError, Message: "Cannot inspect repository .git entry"})
		}
	}

	switch {
	case gitless == 0 && missing == 0 && inspectionErrors == 0:
		report.Add(doctor.DiagnosticResult{
			Category: "Workspace",
			Name:     "Git trees",
			Status:   doctor.StatusOK,
			Message:  fmt.Sprintf("all %d manifest paths have .git (repository integrity unverified)", len(targets)),
		})
	default:
		if gitless > 0 {
			report.Add(doctor.DiagnosticResult{
				Category:      "Workspace",
				Name:          "Gitless trees",
				Status:        doctor.StatusWarning,
				Message:       fmt.Sprintf("%d manifest path(s) have files but no .git", gitless),
				FixSuggestion: "orbit repair",
			})
		}
		if missing > 0 {
			report.Add(doctor.DiagnosticResult{
				Category:      "Workspace",
				Name:          "Uncloned repos",
				Status:        doctor.StatusWarning,
				Message:       fmt.Sprintf("%d manifest path(s) are not cloned", missing),
				FixSuggestion: "orbit init all",
			})
		}
	}
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
		case doctor.StatusWarning, doctor.StatusUnverified:
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
