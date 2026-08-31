package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var manifestFlag string

	cmd := &cobra.Command{
		Use:   "status [scope]",
		Short: "Show git status and working tree cleanliness across workspace repositories",
		Long:  "Inspects all cloned repositories and renders a colorized table of branch names, sync states (ahead/behind), and uncommitted changes.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Workspace Status — Scope: %s", scope)))

			if _, err := os.Stat(manifestPath); err != nil {
				return fmt.Errorf("manifest file not found at %s. Ensure workspace.yaml exists or pass --manifest", manifestPath)
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return fmt.Errorf("failed to load workspace manifest: %w", err)
			}

			targets := m.ResolveScope(scope)
			if len(targets) == 0 {
				fmt.Fprintf(out, "No repositories found for scope %q.\n", scope)
				return nil
			}

			statuses := orchestrator.GetWorkspaceStatus(workspaceRoot, targets)

			tbl := table.New(
				table.Column{Title: "REPOSITORY", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 16},
				table.Column{Title: "PATH", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 20, Flexible: true},
				table.Column{Title: "BRANCH", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 8},
				table.Column{Title: "SYNC", HeaderStyle: headerStyle, MinWidth: 12},
				table.Column{Title: "WORKING TREE", HeaderStyle: headerStyle, MinWidth: 14},
			)

			cleanCount := 0
			dirtyCount := 0
			missingCount := 0
			gitlessCount := 0
			otherErrCount := 0

			for _, s := range statuses {
				repoCell := table.PlainCell(s.Name)
				pathCell := table.PlainCell(s.Path)

				switch s.Error {
				case "":
					branchCell := table.PlainCell(s.CurrentBranch)
					var syncCell table.Cell
					switch {
					case s.AheadCount > 0 && s.BehindCount > 0:
						syncCell = table.StyleCell(fmt.Sprintf("↕%d/%d diverged", s.AheadCount, s.BehindCount), warningStyle)
					case s.AheadCount > 0:
						syncCell = table.StyleCell(fmt.Sprintf("↑%d ahead", s.AheadCount), subtleStyle)
					case s.BehindCount > 0:
						syncCell = table.StyleCell(fmt.Sprintf("↓%d behind", s.BehindCount), subtleStyle)
					default:
						syncCell = table.StyleCell("up to date", subtleStyle)
					}

					var treeCell table.Cell
					if s.IsClean {
						cleanCount++
						treeCell = table.StyleCell("✔ clean", successStyle)
					} else {
						dirtyCount++
						treeCell = table.StyleCell(fmt.Sprintf("✖ dirty (%d)", s.ModifiedCount), errorStyle)
					}

					tbl.AddStyledRow(repoCell, pathCell, branchCell, syncCell, treeCell)
				case orchestrator.ErrMissing:
					missingCount++
					tbl.AddStyledRow(repoCell, pathCell, table.StyleCell("-", subtleStyle), table.StyleCell("-", subtleStyle), table.StyleCell("not cloned", subtleStyle))
				case orchestrator.ErrGitless:
					gitlessCount++
					tbl.AddStyledRow(repoCell, pathCell, table.StyleCell("-", subtleStyle), table.StyleCell("-", subtleStyle), table.StyleCell("gitless — run orbit repair", warningStyle))
				default:
					otherErrCount++
					tbl.AddStyledRow(repoCell, pathCell, table.StyleCell("-", subtleStyle), table.StyleCell("-", subtleStyle), table.StyleCell(s.Error, errorStyle))
				}
			}

			fmt.Fprintln(out)
			_ = tbl.Render(out)

			// Summary footer
			fmt.Fprintf(out, "\n%s  %s  %s  %s  %s\n",
				infoStyle.Render(fmt.Sprintf("Total: %d", len(statuses))),
				successStyle.Render(fmt.Sprintf("✔ %d clean", cleanCount)),
				warningStyle.Render(fmt.Sprintf("⚠ %d dirty", dirtyCount)),
				subtleStyle.Render(fmt.Sprintf("○ %d not cloned", missingCount)),
				warningStyle.Render(fmt.Sprintf("✖ %d gitless", gitlessCount)),
			)
			if otherErrCount > 0 {
				fmt.Fprintf(out, "%s\n", errorStyle.Render(fmt.Sprintf("  %d other errors", otherErrCount)))
			}
			if gitlessCount > 0 {
				fmt.Fprintf(out, "  %s  %s\n", iconInfo, subtleStyle.Render("gitless trees: orbit repair [scope]  (copies .git only; never checkout -f)"))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")

	return cmd
}
