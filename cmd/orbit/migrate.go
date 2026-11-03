package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	migrateapp "github.com/manovaspace/orbit-cli/pkg/migrate/application"
	migratedomain "github.com/manovaspace/orbit-cli/pkg/migrate/domain"
	"github.com/manovaspace/orbit-cli/pkg/migrate/infrastructure/statefile"
	"github.com/manovaspace/orbit-cli/pkg/migrate/infrastructure/workspace"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

// The migrate commands reach git and the state file through these seams so
// tests can swap the adapters.
var (
	migrateRunner runner.Runner         = runner.Exec{}
	migrateStore  migrateapp.StateStore = statefile.New()
)

func migrateBootstrap() *workspace.Bootstrapper {
	return workspace.New(migrateRunner)
}

func newMigrateEngine(workspaceRoot string) *migrateapp.Engine {
	return migrateapp.NewEngine(workspaceRoot, "", migrateStore)
}

// runWorkspaceMigrations applies pending migrations for commands outside this
// group (init, update).
func runWorkspaceMigrations(workspaceRoot string) ([]migratedomain.MigrationResult, error) {
	res, err := newMigrateEngine(workspaceRoot).RunPending()
	return res.Results, err
}

// workspaceMigrations exposes the built-in migration list to init.
func workspaceMigrations() []migratedomain.Migration {
	return migratedomain.GetBuiltinMigrations()
}

// setupWorkspace runs the workspace bootstrap used by init.
func setupWorkspace(workspaceRoot string, repoPaths ...string) error {
	return migrateBootstrap().SetupWorkspace(workspaceRoot, repoPaths...)
}

func newMigrateCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run and inspect workspace state migrations",
		Long:  "Executes pending workspace migrations and tracks applied state in .orbit/migrations.json.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			workspaceRoot := findWorkspaceRoot("")

			res, err := newMigrateEngine(workspaceRoot).RunPending()
			if err != nil {
				runErr := clierr.Wrap(err, clierr.Failed("migration run failed"))
				writeMigrateFailure(out, jsonFlag, runErr)
				return runErr
			}

			if jsonFlag {
				return output.Write(out, output.Success(res))
			}

			p := render.NewPrinter(out)
			renderMigrateApply(p, res)
			return p.Err()
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	cmd.AddCommand(newMigrateStatusCmd())

	return cmd
}

func newMigrateStatusCmd() *cobra.Command {
	var (
		pageFlag  int
		limitFlag int
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of all registered workspace migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			workspaceRoot := findWorkspaceRoot("")

			engine := newMigrateEngine(workspaceRoot)
			res, err := engine.Status()
			if err != nil {
				statusErr := clierr.Wrap(err, clierr.Failed("failed to load migration state"))
				writeMigrateFailure(out, jsonFlag, statusErr)
				return statusErr
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}

			p := render.NewPrinter(out)
			renderMigrateStatus(p, res, pageFlag, limitFlag)
			return p.Err()
		},
	}

	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum rows per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func writeMigrateFailure(out io.Writer, jsonFlag bool, err error) {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
}

// renderMigrateApply writes the text form of one `orbit migrate` run.
func renderMigrateApply(p *render.Printer, res migrateapp.ApplyResult) {
	p.Title("Workspace migrations")
	p.KV("Workspace root", res.WorkspaceRoot)

	skipped, failed := 0, 0
	for _, result := range res.Results {
		switch {
		case result.Success:
			p.Line(render.StatusOK, fmt.Sprintf("%s: %s", result.ID, result.Description))
		case result.Skipped:
			skipped++
			p.Line(render.StatusWarn, fmt.Sprintf("%s: skipped", result.ID))
		default:
			failed++
			message := result.Error
			if message == "" {
				message = "failed"
			}
			p.Line(render.StatusFail, fmt.Sprintf("%s: %s", result.ID, message))
		}
	}
	if len(res.Results) == 0 {
		p.Empty(render.Plural(0, "migration", "migrations"))
	}

	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: res.Applied, Label: "applied"},
		render.SummaryPart{Status: render.StatusWarn, Count: skipped, Label: "skipped"},
		render.SummaryPart{Status: render.StatusFail, Count: failed, Label: "failed"},
	)
}

// renderMigrateStatus writes the table and summary for `orbit migrate status`.
func renderMigrateStatus(p *render.Printer, res migrateapp.StatusResult, page, limit int) {
	p.Title("Workspace migrations")
	p.KV("State file", res.StatePath)

	if len(res.Entries) == 0 {
		p.Empty(render.Plural(0, "migration", "migrations"))
	} else {
		tbl := table.New(
			table.Column{Title: "MIGRATION ID", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 24},
			table.Column{Title: "DESCRIPTION", HeaderStyle: render.Header, CellStyle: render.Subtle, MinWidth: 30, Flexible: true},
			table.Column{Title: "STATUS", HeaderStyle: render.Header, MinWidth: 16},
		)
		if limit > 0 {
			tbl.WithPagination(page, limit)
		}
		for _, entry := range res.Entries {
			var status string
			if entry.Applied && entry.AppliedAt != nil {
				status = fmt.Sprintf("%s applied (%s)", render.StatusOK.Icon(), entry.AppliedAt.Format("2006-01-02 15:04"))
			} else {
				status = render.StatusWarn.Icon() + " pending"
			}
			tbl.AddRow(entry.ID, entry.Description, status)
		}
		_ = p.Table(tbl)
	}

	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: res.Applied, Label: "applied"},
		render.SummaryPart{Status: render.StatusWarn, Count: res.Pending, Label: "pending"},
	)
}
