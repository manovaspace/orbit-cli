package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/env"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		manifestFlag   string
		skipSelfUpdate bool
		skipSync       bool
		skipMigrate    bool
		skipEnv        bool
		concurrency    int
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Unified update (CLI, git repos, migrations, env)",
		Long: `Performs a full workspace synchronization and verification:
  1. Checks for manova CLI updates
  2. Synchronizes all clean default git branches from origin
  3. Executes pending workspace migrations (.manova/state.json)
  4. Validates project .env files against .env.schema.yaml contracts`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			fmt.Fprintln(out, titleStyle.Render("Manova Unified Workspace Update"))
			fmt.Fprintf(out, "  Workspace Root: %s\n\n", subtleStyle.Render(workspaceRoot))

			// Phase 1: CLI Version Check
			if !skipSelfUpdate {
				fmt.Fprintln(out, headerStyle.Render("── 1/4 CLI Version Check ────────────────────────────────────"))
				res, err := updater.CheckUpdate(version, "", "")
				if err != nil {
					fmt.Fprintf(out, "  %s  Could not check for CLI updates: %v\n", iconWarn, err)
				} else if res.HasUpdate {
					fmt.Fprintf(out, "  %s  %s (Current: %s %s Latest: %s)\n     %s Run %s to upgrade.\n",
						iconInfo,
						warningStyle.Render("A newer version of manova CLI is available!"),
						codeStyle.Render(version),
						iconArrow,
						codeStyle.Render(res.LatestVersion),
						iconArrow,
						boldStyle.Render("manova self-update"),
					)
				} else {
					fmt.Fprintf(out, "  %s  CLI is up to date (%s)\n", iconOK, subtleStyle.Render("v"+version))
				}
				fmt.Fprintln(out)
			}

			// Phase 2: Git Sync
			if !skipSync {
				fmt.Fprintln(out, headerStyle.Render("── 2/4 Workspace Git Sync ───────────────────────────────────"))
				if _, err := os.Stat(manifestPath); err != nil {
					fmt.Fprintf(out, "  %s  Manifest %s not found (skipping git sync)\n", iconWarn, manifestPath)
				} else {
					m, err := manifest.Load(manifestPath)
					if err != nil {
						fmt.Fprintf(out, "  %s  Failed to parse manifest: %v\n", iconError, err)
					} else {
						targets := m.AllRepos()
						syncResults := orchestrator.SyncTargets(workspaceRoot, targets, concurrency)

						upToDate := 0
						fastForwarded := 0
						skipped := 0
						failed := 0

						for _, sr := range syncResults {
							if sr.Success {
								if sr.FastForwarded {
									fastForwarded++
									fmt.Fprintf(out, "  %s  %s: fast-forwarded\n", iconOK, boldStyle.Render(sr.Name))
								} else {
									upToDate++
								}
							} else if sr.SkippedReason != "" {
								skipped++
							} else {
								failed++
								fmt.Fprintf(out, "  %s  %s: %s\n", iconError, boldStyle.Render(sr.Name), sr.Error)
							}
						}

						fmt.Fprintf(out, "  %s  Synced %d repos: %d up to date, %d updated, %d skipped, %d failed\n",
							iconOK, len(targets), upToDate, fastForwarded, skipped, failed)
					}
				}
				fmt.Fprintln(out)
			}

			// Phase 3: Workspace Migrations
			if !skipMigrate {
				fmt.Fprintln(out, headerStyle.Render("── 3/4 Workspace Migrations ─────────────────────────────────"))
				migResults, err := migrate.RunPendingMigrations(workspaceRoot)
				if err != nil {
					fmt.Fprintf(out, "  %s  Migration error: %v\n", iconError, err)
				} else if len(migResults) == 0 {
					fmt.Fprintf(out, "  %s  All migrations up to date (0 pending)\n", iconOK)
				} else {
					for _, mr := range migResults {
						if mr.Success {
							fmt.Fprintf(out, "  %s  Applied: %s (%s)\n", iconOK, boldStyle.Render(mr.ID), mr.Description)
						} else {
							fmt.Fprintf(out, "  %s  Failed: %s (%s)\n", iconError, boldStyle.Render(mr.ID), mr.Error)
						}
					}
				}
				fmt.Fprintln(out)
			}

			// Phase 4: Environment Validation
			if !skipEnv {
				fmt.Fprintln(out, headerStyle.Render("── 4/4 Environment Validation ───────────────────────────────"))
				schemas := findSchemaFiles(workspaceRoot)
				if len(schemas) == 0 {
					fmt.Fprintf(out, "  %s  No .env.schema.yaml contracts found\n", iconInfo)
				} else {
					validSchemas := 0
					envErrors := 0

					for _, sp := range schemas {
						dir := filepath.Dir(sp)
						relDir, _ := filepath.Rel(workspaceRoot, dir)
						schema, err := env.LoadSchema(sp)
						if err != nil {
							envErrors++
							continue
						}

						envPath := filepath.Join(dir, ".env")
						vErrs := env.Validate(envPath, schema)
						if len(vErrs) == 0 {
							validSchemas++
						} else {
							envErrors += len(vErrs)
							fmt.Fprintf(out, "  %s  %s: %d validation error(s)\n", iconError, boldStyle.Render(relDir), len(vErrs))
						}
					}

					if envErrors == 0 {
						fmt.Fprintf(out, "  %s  All %d environment files valid against schemas\n", iconOK, validSchemas)
					} else {
						fmt.Fprintf(out, "  %s  %d environment validation error(s) detected. Run 'manova env check' for details.\n", iconWarn, envErrors)
					}
				}
				fmt.Fprintln(out)
			}

			fmt.Fprintln(out, successStyle.Render("✔ Workspace update process completed."))
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().BoolVar(&skipSelfUpdate, "skip-selfupdate", false, "Skip CLI update check")
	cmd.Flags().BoolVar(&skipSync, "skip-sync", false, "Skip workspace git sync")
	cmd.Flags().BoolVar(&skipMigrate, "skip-migrate", false, "Skip workspace migrations")
	cmd.Flags().BoolVar(&skipEnv, "skip-env", false, "Skip environment validation")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Concurrent git sync workers")

	return cmd
}
