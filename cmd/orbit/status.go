package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
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

			// Render Table Header
			fmt.Fprintf(out, "\n  %-22s %-28s %-16s %-16s %s\n",
				headerStyle.Render("REPOSITORY"),
				headerStyle.Render("PATH"),
				headerStyle.Render("BRANCH"),
				headerStyle.Render("SYNC"),
				headerStyle.Render("WORKING TREE"),
			)
			fmt.Fprintln(out, subtleStyle.Render("  ─────────────────────────────────────────────────────────────────────────────────────────────"))

			cleanCount := 0
			dirtyCount := 0
			missingCount := 0
			gitlessCount := 0
			otherErrCount := 0

			for _, s := range statuses {
				repoCol := boldStyle.Render(padRight(s.Name, 22))
				pathCol := subtleStyle.Render(padRight(s.Path, 28))

				switch s.Error {
				case "":
					// ok
				case orchestrator.ErrMissing:
					missingCount++
					fmt.Fprintf(out, "  %-22s %-28s %-16s %-16s %s\n",
						repoCol,
						pathCol,
						subtleStyle.Render("-"),
						subtleStyle.Render("-"),
						subtleStyle.Render("not cloned"),
					)
					continue
				case orchestrator.ErrGitless:
					gitlessCount++
					fmt.Fprintf(out, "  %-22s %-28s %-16s %-16s %s\n",
						repoCol,
						pathCol,
						subtleStyle.Render("-"),
						subtleStyle.Render("-"),
						warningStyle.Render("gitless — run orbit repair"),
					)
					continue
				default:
					otherErrCount++
					fmt.Fprintf(out, "  %-22s %-28s %-16s %-16s %s\n",
						repoCol,
						pathCol,
						subtleStyle.Render("-"),
						subtleStyle.Render("-"),
						errorStyle.Render(s.Error),
					)
					continue
				}

				// Branch
				branchCol := infoStyle.Render(padRight(s.CurrentBranch, 16))

				// Sync state (Ahead/Behind)
				var syncStr string
				if s.AheadCount > 0 && s.BehindCount > 0 {
					syncStr = warningStyle.Render(fmt.Sprintf("↑%d ↓%d", s.AheadCount, s.BehindCount))
				} else if s.AheadCount > 0 {
					syncStr = infoStyle.Render(fmt.Sprintf("↑%d ahead", s.AheadCount))
				} else if s.BehindCount > 0 {
					syncStr = warningStyle.Render(fmt.Sprintf("↓%d behind", s.BehindCount))
				} else {
					syncStr = subtleStyle.Render("up to date")
				}
				syncCol := padRight(syncStr, 16)

				// Working Tree state
				var treeStr string
				if s.IsClean {
					cleanCount++
					treeStr = successStyle.Render("✔ clean")
				} else {
					dirtyCount++
					treeStr = warningStyle.Render(fmt.Sprintf("⚠ %d modified", s.ModifiedCount))
				}

				fmt.Fprintf(out, "  %-22s %-28s %-16s %-16s %s\n",
					repoCol,
					pathCol,
					branchCol,
					syncCol,
					treeStr,
				)
			}

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
