package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/output"
	workspaceapp "github.com/manovaspace/orbit-cli/pkg/workspace/application"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
	"github.com/spf13/cobra"
)

type initSummary struct {
	Cloned         int `json:"cloned"`
	AlreadyPresent int `json:"alreadyPresent"`
	Failed         int `json:"failed"`
}

type initMigrationJSON struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

type initJSON struct {
	Scope          string                        `json:"scope"`
	WorkspaceRoot  string                        `json:"workspaceRoot"`
	ManifestPath   string                        `json:"manifestPath"`
	Bootstrap      bool                          `json:"bootstrap"`
	Results        []workspacedomain.CloneResult `json:"results"`
	Summary        initSummary                   `json:"summary"`
	SetupError     string                        `json:"setupError,omitempty"`
	Migrations     []initMigrationJSON           `json:"migrations,omitempty"`
	MigrationError string                        `json:"migrationError,omitempty"`
	AssetsError    string                        `json:"assetsError,omitempty"`
}

func newInitCmd() *cobra.Command {
	var (
		manifestFlag  string
		concurrency   int
		skipHooks     bool
		bootstrapFlag bool
		jsonFlag      bool
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
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			scope := "core"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			_, manifestExists, statErr := workspaceFS.Stat(manifestPath)
			if statErr != nil || !manifestExists {
				if bootstrapFlag {
					if jsonFlag {
						return output.Write(out, output.Success(initJSON{
							Scope:         scope,
							WorkspaceRoot: workspaceRoot,
							ManifestPath:  manifestPath,
							Bootstrap:     true,
							Results:       []workspacedomain.CloneResult{},
						}))
					}
					fmt.Fprintln(out, titleStyle.Render("Orbit CLI Bootstrap"))
					fmt.Fprintf(out, "  %s  CLI installed successfully.\n", iconOK)
					fmt.Fprintf(out, "  %s  Run '%s' to begin setup, or run '%s' inside a cloned workspace.\n\n",
						iconInfo,
						boldStyle.Render("orbit onboard"),
						boldStyle.Render("orbit init"),
					)
					return nil
				}
				manifestErr := clierr.Failed(fmt.Sprintf("manifest file not found at %s. Ensure workspace.yaml exists or pass --manifest", manifestPath))
				if jsonFlag {
					_ = output.Write(out, output.Failure(manifestErr))
				}
				return manifestErr
			}

			if !jsonFlag {
				fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Workspace Init — Scope: %s", scope)))
				fmt.Fprintf(out, "  Workspace Root: %s\n", subtleStyle.Render(workspaceRoot))
				fmt.Fprintf(out, "  Manifest Path:  %s\n\n", subtleStyle.Render(manifestPath))
			}

			res, err := newWorkspaceService(uiOut).Init(cmd.Context(), workspaceapp.InitInput{
				WorkspaceRoot: workspaceRoot,
				ManifestPath:  manifestPath,
				Scope:         scope,
				Concurrency:   concurrency,
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}

			data := initJSON{
				Scope:         scope,
				WorkspaceRoot: workspaceRoot,
				ManifestPath:  manifestPath,
				Results:       res.Results,
				Summary: initSummary{
					Cloned:         res.Cloned,
					AlreadyPresent: res.AlreadyPresent,
					Failed:         res.Failed,
				},
			}

			if len(res.Targets) == 0 {
				if jsonFlag {
					return output.Write(out, output.Success(data))
				}
				fmt.Fprintf(out, "No repositories found for scope %q.\n", scope)
				return nil
			}

			// Post-clone setup & workspace initialization
			if !skipHooks {
				if !jsonFlag {
					fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Post-Clone Setup & Workspace Initialization ─────────────"))
				}
				var selectedRepoPaths []string
				for _, target := range res.Targets {
					selectedRepoPaths = append(selectedRepoPaths, target.Path)
				}
				if err := migrate.SetupWorkspace(workspaceRoot, selectedRepoPaths...); err != nil {
					data.SetupError = err.Error()
					if jsonFlag {
						setupErr := clierr.Wrap(err, clierr.Failed("repositories processed; workspace setup incomplete"))
						_ = output.Write(out, output.Failure(setupErr))
						return setupErr
					}
					fmt.Fprintf(out, "  %s  Workspace setup incomplete: %v\n", iconError, err)
					return fmt.Errorf("repositories processed; workspace setup incomplete: %w", err)
				}
				if !jsonFlag {
					fmt.Fprintf(out, "  %s  Workspace directory structure, hooks, and Cursor rules initialized.\n", iconOK)
				}

				if len(migrate.GetBuiltinMigrations()) > 0 {
					migResults, err := migrate.RunPendingMigrations(workspaceRoot)
					if err != nil {
						data.MigrationError = err.Error()
						if !jsonFlag {
							fmt.Fprintf(out, "  %s  Workspace migration warning: %v\n", iconWarn, err)
						}
					} else {
						for _, mr := range migResults {
							data.Migrations = append(data.Migrations, initMigrationJSON{
								ID:          mr.ID,
								Description: mr.Description,
								Success:     mr.Success,
								Error:       mr.Error,
							})
							if mr.Success && !jsonFlag {
								fmt.Fprintf(out, "  %s  Migration applied: %s (%s)\n", iconOK, boldStyle.Render(mr.ID), mr.Description)
							}
						}
					}
				}
			}

			if !jsonFlag {
				// Summary
				fmt.Fprintf(out, "\n%s  %s  %s\n",
					successStyle.Render(fmt.Sprintf("✔ %d cloned", res.Cloned)),
					infoStyle.Render(fmt.Sprintf("ℹ %d already present", res.AlreadyPresent)),
					errorStyle.Render(fmt.Sprintf("✖ %d failed", res.Failed)),
				)
			}

			if res.Failed > 0 {
				cloneErr := clierr.Failed(fmt.Sprintf("clone completed with %d error(s)", res.Failed))
				if jsonFlag {
					_ = output.Write(out, output.Failure(cloneErr))
				}
				return cloneErr
			}

			var rels []string
			for _, t := range res.Targets {
				rels = append(rels, t.Path)
			}
			if err := pullWorkspaceAssets(cmd.Context(), workspaceRoot, rels, false); err != nil {
				data.AssetsError = err.Error()
				if !jsonFlag {
					fmt.Fprintf(out, "  %s  Assets: %s\n", iconWarn, err.Error())
				}
			}

			if jsonFlag {
				return output.Write(out, output.Success(data))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent clone workers")
	cmd.Flags().BoolVar(&skipHooks, "skip-hooks", false, "Skip post-clone workspace hooks and migrations")
	cmd.Flags().BoolVar(&bootstrapFlag, "bootstrap", false, "Bootstrap workspace without failing if workspace.yaml is not yet cloned")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
