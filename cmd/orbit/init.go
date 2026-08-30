package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		manifestFlag  string
		concurrency   int
		skipHooks     bool
		bootstrapFlag bool
	)

	cmd := &cobra.Command{
		Use:   "init [scope]",
		Short: "Clone and initialize workspace repositories",
		Long: `Resolves repository targets from workspace.yaml and clones them concurrently.
Scopes:
  core            - Clones essential core baseline (default)
  all / *         - Clones all repositories in the manifest
  orbit           - Clones Orbit platform toolkit
  manovaspace     - Clones open-source commons
  clients/<name>  - Clones a specific client cluster
  <repo-name>     - Clones an individual repository`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			scope := "core"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			if _, err := os.Stat(manifestPath); err != nil {
				if bootstrapFlag {
					fmt.Fprintln(out, titleStyle.Render("Orbit CLI Bootstrap"))
					fmt.Fprintf(out, "  %s  CLI installed successfully.\n", iconOK)
					fmt.Fprintf(out, "  %s  Run '%s' to begin setup, or run '%s' inside a cloned workspace.\n\n",
						iconInfo,
						boldStyle.Render("orbit onboard"),
						boldStyle.Render("orbit init"),
					)
					return nil
				}
				return fmt.Errorf("manifest file not found at %s. Ensure workspace.yaml exists or pass --manifest", manifestPath)
			}

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Workspace Init — Scope: %s", scope)))
			fmt.Fprintf(out, "  Workspace Root: %s\n", subtleStyle.Render(workspaceRoot))
			fmt.Fprintf(out, "  Manifest Path:  %s\n\n", subtleStyle.Render(manifestPath))

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return fmt.Errorf("failed to load workspace manifest: %w", err)
			}

			targets, err := m.ResolveRepos(scope)
			if err != nil {
				return fmt.Errorf("failed to resolve repositories for scope %q: %w", scope, err)
			}

			if len(targets) == 0 {
				fmt.Fprintf(out, "No repositories found for scope %q.\n", scope)
				return nil
			}

			fmt.Fprintf(out, "%s\n", headerStyle.Render(fmt.Sprintf("Cloning %d repositories (concurrency: %d)...", len(targets), concurrency)))

			clonedCount := 0
			existCount := 0
			failCount := 0

			results := orchestrator.CloneTargets(workspaceRoot, targets, concurrency, func(res orchestrator.CloneResult) {
				if res.Success {
					if res.AlreadyExists {
						fmt.Fprintf(out, "  %s  %s %s\n", iconOK, boldStyle.Render(res.Name), subtleStyle.Render("(already exists)"))
					} else {
						fmt.Fprintf(out, "  %s  %s %s\n", iconOK, boldStyle.Render(res.Name), successStyle.Render("(cloned)"))
					}
				} else {
					fmt.Fprintf(out, "  %s  %s %s\n", iconError, boldStyle.Render(res.Name), errorStyle.Render(fmt.Sprintf("(failed: %s)", res.Error)))
				}
			})

			for _, res := range results {
				if res.Success {
					if res.AlreadyExists {
						existCount++
					} else {
						clonedCount++
					}
				} else {
					failCount++
				}
			}

			// Post-clone setup & workspace initialization
			if !skipHooks {
				fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Post-Clone Setup & Workspace Initialization ─────────────"))
				if err := migrate.SetupWorkspace(workspaceRoot); err != nil {
					fmt.Fprintf(out, "  %s  Workspace setup warning: %v\n", iconWarn, err)
				} else {
					fmt.Fprintf(out, "  %s  Workspace directory structure, hooks, and Cursor rules initialized.\n", iconOK)
				}

				if len(migrate.GetBuiltinMigrations()) > 0 {
					migResults, err := migrate.RunPendingMigrations(workspaceRoot)
					if err != nil {
						fmt.Fprintf(out, "  %s  Workspace migration warning: %v\n", iconWarn, err)
					} else {
						for _, mr := range migResults {
							if mr.Success {
								fmt.Fprintf(out, "  %s  Migration applied: %s (%s)\n", iconOK, boldStyle.Render(mr.ID), mr.Description)
							}
						}
					}
				}
			}

			// Summary
			fmt.Fprintf(out, "\n%s  %s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d cloned", clonedCount)),
				infoStyle.Render(fmt.Sprintf("ℹ %d already present", existCount)),
				errorStyle.Render(fmt.Sprintf("✖ %d failed", failCount)),
			)

			if failCount > 0 {
				return fmt.Errorf("clone completed with %d error(s)", failCount)
			}

			var rels []string
			for _, t := range targets {
				rels = append(rels, t.Path)
			}
			if err := pullWorkspaceAssets(cmd.Context(), workspaceRoot, rels, false); err != nil {
				fmt.Fprintf(out, "  %s  Assets: %s\n", iconWarn, err.Error())
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent clone workers")
	cmd.Flags().BoolVar(&skipHooks, "skip-hooks", false, "Skip post-clone workspace hooks and migrations")
	cmd.Flags().BoolVar(&bootstrapFlag, "bootstrap", false, "Bootstrap workspace without failing if workspace.yaml is not yet cloned")

	return cmd
}
