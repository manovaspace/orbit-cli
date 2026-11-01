package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	workspaceapp "github.com/manovaspace/orbit-cli/pkg/workspace/application"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
	"github.com/spf13/cobra"
)

type syncSummary struct {
	Updated  int `json:"updated"`
	UpToDate int `json:"upToDate"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

type syncJSON struct {
	Scope      string                       `json:"scope"`
	Results    []workspacedomain.SyncResult `json:"results"`
	Summary    syncSummary                  `json:"summary"`
	AssetError string                       `json:"assetError,omitempty"`
}

func summarizeSyncResults(results []workspacedomain.SyncResult) syncSummary {
	summary := syncSummary{}
	for _, res := range results {
		switch {
		case res.Success && res.FastForwarded:
			summary.Updated++
		case res.Success:
			summary.UpToDate++
		case res.SkippedReason != "":
			summary.Skipped++
		default:
			summary.Failed++
		}
	}
	return summary
}

func newSyncCmd() *cobra.Command {
	var (
		manifestFlag string
		concurrency  int
		jsonFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "sync [scope]",
		Short: "Fetch and fast-forward clean default branches across workspace repositories",
		Long:  "Fetches upstream origin for all targets, verifies clean working tree, and performs fast-forward merges on default branches without overwriting uncommitted work.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			if !jsonFlag {
				fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Workspace Sync — Scope: %s", scope)))
			}

			res, err := newWorkspaceService(uiOut).Sync(cmd.Context(), workspaceapp.SyncInput{
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

			if len(res.Targets) == 0 {
				if jsonFlag {
					return output.Write(out, output.Success(syncJSON{
						Scope:   res.Scope,
						Results: res.Results,
						Summary: summarizeSyncResults(res.Results),
					}))
				}
				fmt.Fprintf(out, "No repositories found for scope %q.\n", scope)
				return nil
			}

			summary := summarizeSyncResults(res.Results)
			assetError := ""

			var rels []string
			for _, t := range res.Targets {
				rels = append(rels, t.Path)
			}
			if err := pullWorkspaceAssets(cmd.Context(), workspaceRoot, rels, false); err != nil {
				assetError = err.Error()
				summary.Failed++
				if !jsonFlag {
					fmt.Fprintf(out, "  %s  %s\n", iconError, errorStyle.Render("Assets: "+assetError))
				}
			}

			if !jsonFlag {
				// Summary footer
				fmt.Fprintf(out, "\n%s  %s  %s  %s\n",
					successStyle.Render(fmt.Sprintf("✔ %d updated", summary.Updated)),
					infoStyle.Render(fmt.Sprintf("✔ %d up to date", summary.UpToDate)),
					warningStyle.Render(fmt.Sprintf("⚠ %d skipped", summary.Skipped)),
					errorStyle.Render(fmt.Sprintf("✖ %d failed", summary.Failed)),
				)
			}

			if summary.Failed > 0 {
				syncErr := clierr.Failed(fmt.Sprintf("sync completed with %d error(s)", summary.Failed))
				if jsonFlag {
					_ = output.Write(out, output.Failure(syncErr))
				}
				return syncErr
			}
			if jsonFlag {
				return output.Write(out, output.Success(syncJSON{
					Scope:      res.Scope,
					Results:    res.Results,
					Summary:    summary,
					AssetError: assetError,
				}))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent sync workers")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
