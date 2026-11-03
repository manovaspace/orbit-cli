package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/table"
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
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			res, err := newWorkspaceService(io.Discard).Sync(cmd.Context(), workspaceapp.SyncInput{
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
				p := render.NewPrinter(out)
				renderSyncResult(p, scope, res.Results, "")
				return p.Err()
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
			}

			if summary.Failed > 0 {
				syncErr := clierr.Failed(fmt.Sprintf("sync completed with %d error(s)", summary.Failed))
				if jsonFlag {
					_ = output.Write(out, output.Failure(syncErr))
					return syncErr
				}
				p := render.NewPrinter(out)
				renderSyncResult(p, scope, res.Results, assetError)
				if perr := p.Err(); perr != nil {
					return perr
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

			p := render.NewPrinter(out)
			renderSyncResult(p, scope, res.Results, assetError)
			return p.Err()
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent sync workers")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

// renderSyncResult writes the per-repository rows the service used to print
// while it ran, followed by the shared summary line.
func renderSyncResult(p *render.Printer, scope string, results []workspacedomain.SyncResult, assetError string) {
	p.Title(fmt.Sprintf("Workspace sync — %s", scope))

	if len(results) > 0 {
		tbl := table.New(
			table.Column{Title: "REPOSITORY", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 16},
			table.Column{Title: "PATH", HeaderStyle: render.Header, CellStyle: render.Subtle, MinWidth: 20, Flexible: true},
			table.Column{Title: "RESULT", HeaderStyle: render.Header, MinWidth: 24},
		)
		for _, res := range results {
			var result string
			switch {
			case res.Success && res.FastForwarded:
				result = render.StatusOK.Icon() + " fast-forwarded"
			case res.Success:
				result = render.StatusInfo.Icon() + " up to date"
			case res.SkippedReason != "":
				result = render.StatusWarn.Icon() + " skipped: " + res.SkippedReason
			default:
				msg := res.Error
				if msg == "" {
					msg = "unknown sync error"
				}
				result = render.StatusFail.Icon() + " " + msg
			}
			tbl.AddRow(res.Name, res.Path, result)
		}
		_ = p.Table(tbl)
	} else {
		p.Empty(render.Plural(0, "repository", "repositories"))
	}

	if assetError != "" {
		p.Line(render.StatusFail, "Assets: "+assetError)
	}

	summary := summarizeSyncResults(results)
	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: summary.Updated, Label: "updated"},
		render.SummaryPart{Status: render.StatusInfo, Count: summary.UpToDate, Label: "up to date"},
		render.SummaryPart{Status: render.StatusWarn, Count: summary.Skipped, Label: "skipped"},
		render.SummaryPart{Status: render.StatusFail, Count: summary.Failed, Label: "failed"},
	)
}
