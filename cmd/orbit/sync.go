package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var (
		manifestFlag string
		concurrency  int
	)

	cmd := &cobra.Command{
		Use:   "sync [scope]",
		Short: "Fast-forward clean default branches across repos",
		Long:  "Fetches upstream origin for all targets, verifies clean working tree, and performs fast-forward merges on default branches without overwriting uncommitted work.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Workspace Sync — Scope: %s", scope)))

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

			fmt.Fprintf(out, "%s\n\n", headerStyle.Render(fmt.Sprintf("Syncing %d repositories (concurrency: %d)...", len(targets), concurrency)))

			results := orchestrator.SyncTargets(workspaceRoot, targets, concurrency)

			updatedCount := 0
			currentCount := 0
			skippedCount := 0
			errorCount := 0

			for _, res := range results {
				nameCol := boldStyle.Render(padRight(res.Name, 24))

				if res.Success {
					if res.FastForwarded {
						updatedCount++
						fmt.Fprintf(out, "  %s  %s %s\n", iconOK, nameCol, successStyle.Render("Fast-forwarded to latest origin"))
					} else {
						currentCount++
						fmt.Fprintf(out, "  %s  %s %s\n", iconOK, nameCol, subtleStyle.Render("Up to date"))
					}
				} else if res.SkippedReason != "" {
					skippedCount++
					fmt.Fprintf(out, "  %s  %s %s\n", iconWarn, nameCol, warningStyle.Render("Skipped: "+res.SkippedReason))
				} else {
					errorCount++
					errMsg := res.Error
					if errMsg == "" {
						errMsg = "unknown sync error"
					}
					fmt.Fprintf(out, "  %s  %s %s\n", iconError, nameCol, errorStyle.Render("Error: "+errMsg))
				}
			}

			// Summary footer
			fmt.Fprintf(out, "\n%s  %s  %s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d updated", updatedCount)),
				infoStyle.Render(fmt.Sprintf("✔ %d up to date", currentCount)),
				warningStyle.Render(fmt.Sprintf("⚠ %d skipped", skippedCount)),
				errorStyle.Render(fmt.Sprintf("✖ %d failed", errorCount)),
			)

			if errorCount > 0 {
				return fmt.Errorf("sync completed with %d error(s)", errorCount)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent sync workers")

	return cmd
}
