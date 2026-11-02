package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/audit"
	auditapp "github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/spf13/cobra"
)

// auditWorkspacePaths locates workspace.yaml; command tests replace it.
var auditWorkspacePaths = func() (root, manifestPath string) {
	root = findWorkspaceRoot("")
	return root, findManifestPath(root, "")
}

// auditNewService wires the audit adapters; command tests replace it.
var auditNewService = func(root, manifestPath string) *auditapp.Service {
	return audit.NewService(root, manifestPath)
}

func newAuditCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit workspace versions, dependencies and design-system usage",
		Long: "Report drift between the workspace and manovaspace/ci/ci/versions.env, resolved dependency versions and kit exports shadowed by local code. " +
			"The audit is read-only: it never edits a repository and reads the network only with 'audit deps --remote'. " +
			"Exit codes: 0 no ERROR findings, 1 at least one ERROR, 2 something could not be verified.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudit(cmd, jsonOutput, func(ctx context.Context, service *auditapp.Service) audit.Result {
				return service.All(ctx)
			})
		},
	}
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Print the report as a JSON envelope")
	cmd.AddCommand(newAuditVersionsCmd(&jsonOutput))
	cmd.AddCommand(newAuditDepsCmd(&jsonOutput))
	cmd.AddCommand(newAuditUsageCmd(&jsonOutput))
	return cmd
}

func newAuditVersionsCmd(jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use:          "versions",
		Short:        "Compare tool versions with manovaspace/ci/ci/versions.env",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudit(cmd, *jsonOutput, func(ctx context.Context, service *auditapp.Service) audit.Result {
				return service.Versions(ctx)
			})
		},
	}
}

func newAuditDepsCmd(jsonOutput *bool) *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:          "deps [pkg]",
		Short:        "Report resolved dependency versions, forbidden ranges and shadowed exports",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg := auditapp.DefaultPackage
			if len(args) == 1 {
				pkg = args[0]
			}
			return runAudit(cmd, *jsonOutput, func(ctx context.Context, service *auditapp.Service) audit.Result {
				return service.Deps(ctx, pkg, remote)
			})
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "Read the public npm registry for the latest published version")
	return cmd
}

func newAuditUsageCmd(jsonOutput *bool) *cobra.Command {
	var unused bool
	cmd := &cobra.Command{
		Use:          "usage [symbol]",
		Short:        "Verify kit imports against the installed package exports",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := ""
			if len(args) == 1 {
				symbol = args[0]
			}
			return runAudit(cmd, *jsonOutput, func(ctx context.Context, service *auditapp.Service) audit.Result {
				return service.Usage(ctx, symbol, unused)
			})
		},
	}
	cmd.Flags().BoolVar(&unused, "unused", false, "List kit exports nothing imports")
	return cmd
}

// runAudit executes one audit, writes the report and maps the report exit code
// onto a clierr error.
func runAudit(cmd *cobra.Command, jsonOutput bool, run func(context.Context, *auditapp.Service) audit.Result) error {
	out := cmd.OutOrStdout()
	root, manifestPath := auditWorkspacePaths()
	result := run(cmd.Context(), auditNewService(root, manifestPath))
	failure := auditFailure(result.Report)

	if jsonOutput {
		envelope := output.Success(result)
		if failure != nil {
			envelope = output.Failure(failure)
			envelope.Data = result
		}
		if err := output.Write(out, envelope); err != nil {
			return err
		}
		return failure
	}

	renderAuditReport(out, result)
	return failure
}

// auditFailure maps 1 (findings) and 2 (could not verify) onto stable errors.
func auditFailure(report audit.Report) error {
	errorsCount, warnings := report.Counts()
	switch report.ExitCode() {
	case 1:
		return clierr.Failed(fmt.Sprintf("audit found %d error(s) and %d warning(s)", errorsCount, warnings))
	case 2:
		return clierr.Unverified(fmt.Sprintf("audit could not verify %d item(s)", len(report.Unverified)))
	}
	return nil
}

// renderAuditReport prints one line per problem and a final summary. The
// format is intentionally plain: audit output is meant for grep and CI logs.
func renderAuditReport(out io.Writer, result audit.Result) {
	for _, finding := range result.Findings {
		line := fmt.Sprintf("%-5s %s", finding.Severity, finding.ID)
		if finding.Subject != "" {
			line += "  " + finding.Subject
		}
		if finding.Actual != "" {
			line += "  actual " + finding.Actual
		}
		if finding.Expected != "" {
			line += "  expected " + finding.Expected
		}
		switch {
		case finding.Fix == "":
		case strings.HasPrefix(finding.Fix, "excepted: "):
			line += "  " + finding.Fix
		default:
			line += "  fix: " + finding.Fix
		}
		fmt.Fprintln(out, line)
	}
	for _, message := range result.Unverified {
		fmt.Fprintln(out, "UNVERIFIED "+message)
	}
	errorsCount, warnings := result.Counts()
	summary := fmt.Sprintf("summary: %d findings (%d errors, %d warnings), %d unverified",
		len(result.Findings), errorsCount, warnings, len(result.Unverified))
	if result.Detail != "" {
		summary += " — " + result.Detail
	}
	fmt.Fprintln(out, summary)
}
