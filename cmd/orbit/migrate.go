package main

import (
	"fmt"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run and inspect workspace state migrations",
		Long:  "Executes pending workspace migrations and tracks applied state in .orbit/migrations.json.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			workspaceRoot := findWorkspaceRoot("")

			fmt.Fprintln(out, titleStyle.Render("Orbit Workspace Migrations"))
			fmt.Fprintf(out, "  Workspace Root: %s\n\n", subtleStyle.Render(workspaceRoot))

			results, err := migrate.RunPendingMigrations(workspaceRoot)
			if err != nil {
				return fmt.Errorf("migration run failed: %w", err)
			}

			if len(results) == 0 {
				fmt.Fprintln(out, successStyle.Render("✔ All workspace migrations are up to date (0 pending)."))
				return nil
			}

			appliedCount := 0
			for _, r := range results {
				if r.Success {
					appliedCount++
					fmt.Fprintf(out, "  %s  %s: %s\n", iconOK, boldStyle.Render(r.ID), r.Description)
				} else {
					fmt.Fprintf(out, "  %s  %s: %s (%s)\n", iconError, boldStyle.Render(r.ID), errorStyle.Render("Failed"), r.Error)
				}
			}

			fmt.Fprintf(out, "\n%s\n", successStyle.Render(fmt.Sprintf("✔ Applied %d migration(s) successfully.", appliedCount)))
			return nil
		},
	}

	cmd.AddCommand(newMigrateStatusCmd())

	return cmd
}

func newMigrateStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of all registered workspace migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			workspaceRoot := findWorkspaceRoot("")

			engine := migrate.NewEngine(workspaceRoot, "")
			fmt.Fprintln(out, titleStyle.Render("Orbit Workspace Migration Status"))
			fmt.Fprintf(out, "  State File: %s\n\n", subtleStyle.Render(engine.StatePath()))

			state, err := engine.LoadState()
			if err != nil {
				return fmt.Errorf("failed to load migration state: %w", err)
			}

			appliedMap := make(map[string]time.Time)
			for _, rec := range state.Applied {
				appliedMap[rec.ID] = rec.AppliedAt
			}

			allMigrations := migrate.GetBuiltinMigrations()

			tbl := table.New(
				table.Column{Title: "MIGRATION ID", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 24},
				table.Column{Title: "DESCRIPTION", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 30, Flexible: true},
				table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 16},
			)

			pendingCount := 0
			appliedCount := 0

			for _, m := range allMigrations {
				idCell := table.PlainCell(m.ID)
				descCell := table.PlainCell(m.Description)

				var statusCell table.Cell
				if appliedAt, ok := appliedMap[m.ID]; ok {
					appliedCount++
					statusStr := fmt.Sprintf("✔ Applied (%s)", appliedAt.Format("2006-01-02 15:04"))
					statusCell = table.StyleCell(statusStr, successStyle)
				} else {
					pendingCount++
					statusCell = table.StyleCell("⚠ Pending", warningStyle)
				}

				tbl.AddStyledRow(idCell, descCell, statusCell)
			}

			fmt.Fprintln(out)
			_ = tbl.Render(out)

			fmt.Fprintf(out, "\n%s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d applied", appliedCount)),
				warningStyle.Render(fmt.Sprintf("⚠ %d pending", pendingCount)),
			)

			return nil
		},
	}

	return cmd
}
