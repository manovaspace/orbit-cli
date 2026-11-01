package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/env"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/output"
	updaterapp "github.com/manovaspace/orbit-cli/pkg/updater/application"
	updaterdomain "github.com/manovaspace/orbit-cli/pkg/updater/domain"
	"github.com/manovaspace/orbit-cli/pkg/updater/infrastructure/releases"
	workspaceapp "github.com/manovaspace/orbit-cli/pkg/workspace/application"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
	"github.com/spf13/cobra"
)

// releaseUpdater is the seam through which update and self-update reach the
// release API and the binary replacement.
var releaseUpdater updaterapp.Updater = releases.New()

type updateSyncJSON struct {
	ManifestFound bool                         `json:"manifestFound"`
	Results       []workspacedomain.SyncResult `json:"results"`
	UpToDate      int                          `json:"upToDate"`
	FastForwarded int                          `json:"fastForwarded"`
	Skipped       int                          `json:"skipped"`
	Failed        int                          `json:"failed"`
}

type updateMigrationJSON struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

type updateEnvJSON struct {
	Schemas        int `json:"schemas"`
	ValidSchemas   int `json:"validSchemas"`
	ValidationErrs int `json:"validationErrors"`
}

type updateJSON struct {
	SelfUpdate       *updaterdomain.UpdateCheckResult `json:"selfUpdate,omitempty"`
	SelfUpdateError  string                           `json:"selfUpdateError,omitempty"`
	Sync             *updateSyncJSON                  `json:"sync,omitempty"`
	SyncError        string                           `json:"syncError,omitempty"`
	Migrations       []updateMigrationJSON            `json:"migrations,omitempty"`
	MigrationError   string                           `json:"migrationError,omitempty"`
	Environment      *updateEnvJSON                   `json:"environment,omitempty"`
	EnvironmentError string                           `json:"environmentError,omitempty"`
}

func newUpdateCmd() *cobra.Command {
	var (
		manifestFlag   string
		skipSelfUpdate bool
		skipSync       bool
		skipMigrate    bool
		skipEnv        bool
		concurrency    int
		jsonFlag       bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Unified workspace update (CLI, git branches, migrations, and env validation)",
		Long: `Performs a full workspace synchronization and verification:
  1. Checks for Orbit CLI updates
  2. Synchronizes all clean default git branches from origin
  3. Executes pending workspace migrations (.orbit/migrations.json)
  4. Validates project .env files against .env.schema.yaml contracts`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			human := io.Writer(out)
			if jsonFlag {
				human = io.Discard
			}
			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)
			data := updateJSON{}

			if !jsonFlag {
				fmt.Fprintln(out, titleStyle.Render("Orbit Unified Workspace Update"))
				fmt.Fprintf(out, "  Workspace Root: %s\n\n", subtleStyle.Render(workspaceRoot))
			}

			// Phase 1: CLI Version Check
			if !skipSelfUpdate {
				if !jsonFlag {
					fmt.Fprintln(out, headerStyle.Render("── 1/4 CLI Version Check ────────────────────────────────────"))
				}
				res, err := (&updaterapp.Service{Updater: releaseUpdater, Out: io.Discard}).Check(cmd.Context(), updaterapp.CheckInput{CurrentVersion: version})
				if err != nil {
					data.SelfUpdateError = err.Error()
					if !jsonFlag {
						fmt.Fprintf(out, "  %s  Could not check for CLI updates: %v\n", iconWarn, err)
					}
				} else {
					data.SelfUpdate = res
					if !jsonFlag {
						if res.HasUpdate {
							fmt.Fprintf(out, "  %s  %s (Current: %s %s Latest: %s)\n     %s Run %s to upgrade.\n",
								iconInfo,
								warningStyle.Render("A newer version of Orbit CLI is available!"),
								codeStyle.Render(version),
								iconArrow,
								codeStyle.Render(res.LatestVersion),
								iconArrow,
								boldStyle.Render("orbit self-update"),
							)
						} else {
							fmt.Fprintf(out, "  %s  CLI is up to date (%s)\n", iconOK, subtleStyle.Render("v"+version))
						}
					}
				}
				if !jsonFlag {
					fmt.Fprintln(out)
				}
			}

			// Phase 2: Git Sync
			if !skipSync {
				if !jsonFlag {
					fmt.Fprintln(out, headerStyle.Render("── 2/4 Workspace Git Sync ───────────────────────────────────"))
				}
				syncOut, syncErr := newWorkspaceService(io.Discard).SyncAll(cmd.Context(), workspaceapp.SyncAllInput{
					WorkspaceRoot: workspaceRoot,
					ManifestPath:  manifestPath,
					Concurrency:   concurrency,
				})
				syncData := &updateSyncJSON{ManifestFound: syncOut.ManifestFound, Results: syncOut.Results}
				if syncData.Results == nil {
					syncData.Results = []workspacedomain.SyncResult{}
				}
				for _, sr := range syncOut.Results {
					switch {
					case sr.Success && sr.FastForwarded:
						syncData.FastForwarded++
					case sr.Success:
						syncData.UpToDate++
					case sr.SkippedReason != "":
						syncData.Skipped++
					default:
						syncData.Failed++
					}
				}
				data.Sync = syncData

				switch {
				case !syncOut.ManifestFound:
					if !jsonFlag {
						fmt.Fprintf(out, "  %s  Manifest %s not found (skipping git sync)\n", iconWarn, manifestPath)
					}
				case syncErr != nil:
					data.SyncError = syncErr.Error()
					if !jsonFlag {
						fmt.Fprintf(out, "  %s  Failed to parse manifest: %v\n", iconError, syncErr)
					}
				default:
					if !jsonFlag {
						for _, sr := range syncOut.Results {
							switch {
							case sr.Success && sr.FastForwarded:
								fmt.Fprintf(out, "  %s  %s: fast-forwarded\n", iconOK, boldStyle.Render(sr.Name))
							case sr.Success:
							case sr.SkippedReason != "":
							default:
								fmt.Fprintf(out, "  %s  %s: %s\n", iconError, boldStyle.Render(sr.Name), sr.Error)
							}
						}

						fmt.Fprintf(human, "  %s  Synced %d repos: %d up to date, %d updated, %d skipped, %d failed\n",
							iconOK, len(syncOut.Targets), syncData.UpToDate, syncData.FastForwarded, syncData.Skipped, syncData.Failed)
					}
				}
				if !jsonFlag {
					fmt.Fprintln(out)
				}
			}

			// Phase 3: Workspace Migrations
			if !skipMigrate {
				if !jsonFlag {
					fmt.Fprintln(out, headerStyle.Render("── 3/4 Workspace Migrations ─────────────────────────────────"))
				}
				migResults, err := migrate.RunPendingMigrations(workspaceRoot)
				if err != nil {
					data.MigrationError = err.Error()
					if !jsonFlag {
						fmt.Fprintf(out, "  %s  Migration error: %v\n", iconError, err)
					}
				} else if len(migResults) == 0 {
					if !jsonFlag {
						fmt.Fprintf(out, "  %s  All migrations up to date (0 pending)\n", iconOK)
					}
				} else {
					for _, mr := range migResults {
						data.Migrations = append(data.Migrations, updateMigrationJSON{
							ID:          mr.ID,
							Description: mr.Description,
							Success:     mr.Success,
							Error:       mr.Error,
						})
						if !jsonFlag {
							if mr.Success {
								fmt.Fprintf(out, "  %s  Applied: %s (%s)\n", iconOK, boldStyle.Render(mr.ID), mr.Description)
							} else {
								fmt.Fprintf(out, "  %s  Failed: %s (%s)\n", iconError, boldStyle.Render(mr.ID), mr.Error)
							}
						}
					}
				}
				if !jsonFlag {
					fmt.Fprintln(out)
				}
			}

			// Phase 4: Environment Validation
			if !skipEnv {
				if !jsonFlag {
					fmt.Fprintln(out, headerStyle.Render("── 4/4 Environment Validation ───────────────────────────────"))
				}
				schemas, err := env.FindSchemas(workspaceRoot)
				if err != nil {
					envErr := clierr.Wrap(err, clierr.Failed("schema discovery failed"))
					data.EnvironmentError = envErr.Error()
					if jsonFlag {
						_ = output.Write(out, output.Failure(envErr))
					}
					return envErr
				}
				envData := &updateEnvJSON{Schemas: len(schemas)}
				data.Environment = envData
				if len(schemas) == 0 {
					if !jsonFlag {
						fmt.Fprintf(out, "  %s  Environment unverified: no .env.schema.yaml contracts found\n", iconInfo)
					}
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
							if !jsonFlag {
								fmt.Fprintf(out, "  %s  %s: %d validation error(s)\n", iconError, boldStyle.Render(relDir), len(vErrs))
							}
						}
					}

					envData.ValidSchemas = validSchemas
					envData.ValidationErrs = envErrors
					if !jsonFlag {
						if envErrors == 0 {
							fmt.Fprintf(out, "  %s  All %d environment files valid against schemas\n", iconOK, validSchemas)
						} else {
							fmt.Fprintf(out, "  %s  %d environment validation error(s) detected. Run 'orbit env check' for details.\n", iconWarn, envErrors)
						}
					}
				}
				if !jsonFlag {
					fmt.Fprintln(out)
				}
			}

			if jsonFlag {
				return output.Write(out, output.Success(data))
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
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
