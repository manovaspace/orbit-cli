package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/spf13/cobra"
)

func newRepairCmd() *cobra.Command {
	var (
		manifestFlag string
		concurrency  int
	)

	cmd := &cobra.Command{
		Use:   "repair [scope]",
		Short: "Attach .git to gitless workspace trees without overwriting files",
		Long: `Clones each target to a temporary directory and copies .git into the existing
tree. Working files are never checkout -f or reset --hard.

Use this when orbit status reports "gitless". Repositories that already have
.git are skipped. Missing paths are cloned as with orbit init.

Scopes match orbit init / orbit status (default: all).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Workspace Repair — Scope: %s", scope)))

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

			fmt.Fprintf(out, "%s\n\n", headerStyle.Render(fmt.Sprintf("Repairing %d repositories (concurrency: %d)...", len(targets), concurrency)))

			repaired := 0
			skipped := 0
			failed := 0

			results := orchestrator.RepairTargets(workspaceRoot, targets, concurrency, func(res orchestrator.RepairResult) {
				nameCol := boldStyle.Render(padRight(res.Name, 24))
				switch {
				case !res.Success:
					fmt.Fprintf(out, "  %s  %s %s\n", iconError, nameCol, errorStyle.Render(res.Error))
				case res.Skipped != "":
					fmt.Fprintf(out, "  %s  %s %s\n", iconOK, nameCol, subtleStyle.Render(res.Skipped))
				default:
					fmt.Fprintf(out, "  %s  %s %s\n", iconOK, nameCol, successStyle.Render("attached .git"))
				}
			})

			for _, res := range results {
				if !res.Success {
					failed++
				} else if res.Skipped != "" {
					skipped++
				} else {
					repaired++
				}
			}

			fmt.Fprintf(out, "\n%s  %s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d repaired", repaired)),
				infoStyle.Render(fmt.Sprintf("ℹ %d skipped", skipped)),
				errorStyle.Render(fmt.Sprintf("✖ %d failed", failed)),
			)

			if failed > 0 {
				return fmt.Errorf("repair completed with %d error(s)", failed)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent repair workers")

	return cmd
}
