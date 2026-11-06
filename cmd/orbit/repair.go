package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
	workspaceapp "github.com/manovaspace/orbit-cli/pkg/workspace/application"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
	"github.com/spf13/cobra"
)

type repairSummary struct {
	Repaired int `json:"repaired"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

type repairJSON struct {
	Scope   string                         `json:"scope"`
	Results []workspacedomain.RepairResult `json:"results"`
	Summary repairSummary                  `json:"summary"`
}

func newRepairCmd() *cobra.Command {
	var (
		manifestFlag string
		concurrency  int
		jsonFlag     bool
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
			p := render.NewPrinter(out)
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
				p.Title(fmt.Sprintf("Orbit Workspace Repair — Scope: %s", scope))
			}

			res, err := newWorkspaceService(uiOut).Repair(cmd.Context(), workspaceapp.RepairInput{
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

			if len(res.Results) == 0 {
				if jsonFlag {
					return output.Write(out, output.Success(repairJSON{
						Scope:   res.Scope,
						Results: res.Results,
						Summary: repairSummary{Repaired: res.Repaired, Skipped: res.Skipped, Failed: res.Failed},
					}))
				}
				p.Line(render.StatusInfo, fmt.Sprintf("No repositories found for scope %q.", scope))
				return p.Err()
			}

			if !jsonFlag {
				p.Summary(
					render.SummaryPart{Status: render.StatusOK, Count: res.Repaired, Label: "repaired"},
					render.SummaryPart{Status: render.StatusInfo, Count: res.Skipped, Label: "skipped"},
					render.SummaryPart{Status: render.StatusFail, Count: res.Failed, Label: "failed"},
				)
			}

			if res.Failed > 0 {
				repairErr := clierr.Failed(fmt.Sprintf("repair completed with %d error(s)", res.Failed))
				if jsonFlag {
					_ = output.Write(out, output.Failure(repairErr))
				}
				return repairErr
			}
			if jsonFlag {
				return output.Write(out, output.Success(repairJSON{
					Scope:   res.Scope,
					Results: res.Results,
					Summary: repairSummary{Repaired: res.Repaired, Skipped: res.Skipped, Failed: res.Failed},
				}))
			}
			return p.Err()
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent repair workers")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
