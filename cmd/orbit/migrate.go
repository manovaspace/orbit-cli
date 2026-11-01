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
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			workspaceRoot := findWorkspaceRoot("")

			fmt.Fprintln(uiOut, titleStyle.Render("Orbit Workspace Migrations"))
			fmt.Fprintf(uiOut, "  Workspace Root: %s\n\n", subtleStyle.Render(workspaceRoot))

			res, err := newMigrateEngine(workspaceRoot).RunPending()
			if err != nil {
				runErr := clierr.Wrap(err, clierr.Failed("migration run failed"))
				writeMigrateFailure(out, jsonFlag, runErr)
				return runErr
			}

			if len(res.Results) == 0 {
				fmt.Fprintln(uiOut, successStyle.Render("✔ All workspace migrations are up to date (0 pending)."))
				if jsonFlag {
					return output.Write(out, output.Success(res))
				}
				return nil
			}

			for _, result := range res.Results {
				if result.Success {
					fmt.Fprintf(uiOut, "  %s  %s: %s\n", iconOK, boldStyle.Render(result.ID), result.Description)
				} else {
					fmt.Fprintf(uiOut, "  %s  %s: %s (%s)\n", iconError, boldStyle.Render(result.ID), errorStyle.Render("Failed"), result.Error)
				}
			}

			fmt.Fprintf(uiOut, "\n%s\n", successStyle.Render(fmt.Sprintf("✔ Applied %d migration(s) successfully.", res.Applied)))
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
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
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
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

			fmt.Fprintln(uiOut, titleStyle.Render("Orbit Workspace Migration Status"))
			fmt.Fprintf(uiOut, "  State File: %s\n\n", subtleStyle.Render(res.StatePath))

			tbl := table.New(
				table.Column{Title: "MIGRATION ID", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 24},
				table.Column{Title: "DESCRIPTION", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 30, Flexible: true},
				table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 16},
			)
			if limitFlag > 0 {
				tbl.WithPagination(pageFlag, limitFlag)
			}

			for _, entry := range res.Entries {
				idCell := table.PlainCell(entry.ID)
				descCell := table.PlainCell(entry.Description)

				var statusCell table.Cell
				if entry.Applied && entry.AppliedAt != nil {
					statusStr := fmt.Sprintf("✔ Applied (%s)", entry.AppliedAt.Format("2006-01-02 15:04"))
					statusCell = table.StyleCell(statusStr, successStyle)
				} else {
					statusCell = table.StyleCell("⚠ Pending", warningStyle)
				}

				tbl.AddStyledRow(idCell, descCell, statusCell)
			}

			fmt.Fprintln(uiOut)
			_ = tbl.Render(uiOut)

			fmt.Fprintf(uiOut, "\n%s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d applied", res.Applied)),
				warningStyle.Render(fmt.Sprintf("⚠ %d pending", res.Pending)),
			)

			return nil
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
