package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/table"
	workspaceapp "github.com/manovaspace/orbit-cli/pkg/workspace/application"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/gitcli"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/manifestfile"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/osfs"
	"github.com/spf13/cobra"
)

// Seams through which the workspace commands reach git and the filesystem.
var (
	workspaceGit      workspaceapp.Git      = gitcli.New(runner.Exec{})
	workspaceFS       workspaceapp.FS       = osfs.New()
	workspaceManifest workspaceapp.Manifest = manifestfile.New()
)

func newWorkspaceService(out io.Writer) *workspaceapp.Service {
	return &workspaceapp.Service{
		Git:      workspaceGit,
		FS:       workspaceFS,
		Manifest: workspaceManifest,
		Out:      out,
		UI:       workspaceUI(),
	}
}

func workspaceUI() workspaceapp.UI {
	return workspaceapp.UI{
		OK:        iconOK,
		Warn:      iconWarn,
		Error:     iconError,
		Info:      iconInfo,
		Title:     func(text string) string { return titleStyle.Render(text) },
		Header:    func(text string) string { return headerStyle.Render(text) },
		Bold:      func(text string) string { return boldStyle.Render(text) },
		Success:   func(text string) string { return successStyle.Render(text) },
		Warning:   func(text string) string { return warningStyle.Render(text) },
		ErrorText: func(text string) string { return errorStyle.Render(text) },
		InfoText:  func(text string) string { return infoStyle.Render(text) },
		Subtle:    func(text string) string { return subtleStyle.Render(text) },
	}
}

type statusSummary struct {
	Total       int `json:"total"`
	Clean       int `json:"clean"`
	Dirty       int `json:"dirty"`
	Missing     int `json:"missing"`
	Gitless     int `json:"gitless"`
	OtherErrors int `json:"otherErrors"`
}

type statusJSON struct {
	Scope   string                       `json:"scope"`
	Repos   []workspacedomain.RepoStatus `json:"repos"`
	Summary statusSummary                `json:"summary"`
}

func summarizeStatuses(statuses []workspacedomain.RepoStatus) statusSummary {
	summary := statusSummary{Total: len(statuses)}
	for _, s := range statuses {
		switch s.Error {
		case "":
			if s.IsClean {
				summary.Clean++
			} else {
				summary.Dirty++
			}
		case workspacedomain.ErrMissing:
			summary.Missing++
		case workspacedomain.ErrGitless:
			summary.Gitless++
		default:
			summary.OtherErrors++
		}
	}
	return summary
}

func newStatusCmd() *cobra.Command {
	var (
		manifestFlag string
		pageFlag     int
		limitFlag    int
		jsonFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "status [scope]",
		Short: "Show git status and working tree cleanliness across workspace repositories",
		Long:  "Inspects all cloned repositories and renders a colorized table of branch names, sync states (ahead/behind), and uncommitted changes.",
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
				fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Workspace Status — Scope: %s", scope)))
			}

			res, err := newWorkspaceService(uiOut).Status(cmd.Context(), workspaceapp.StatusInput{
				WorkspaceRoot: workspaceRoot,
				ManifestPath:  manifestPath,
				Scope:         scope,
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}

			if jsonFlag {
				return output.Write(out, output.Success(statusJSON{
					Scope:   res.Scope,
					Repos:   res.Repos,
					Summary: summarizeStatuses(res.Repos),
				}))
			}

			if len(res.Repos) == 0 {
				fmt.Fprintf(out, "No repositories found for scope %q.\n", scope)
				return nil
			}

			tbl := table.New(
				table.Column{Title: "REPOSITORY", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 16},
				table.Column{Title: "PATH", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 20, Flexible: true},
				table.Column{Title: "BRANCH", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 8},
				table.Column{Title: "SYNC", HeaderStyle: headerStyle, MinWidth: 12},
				table.Column{Title: "WORKING TREE", HeaderStyle: headerStyle, MinWidth: 14},
			)
			if limitFlag > 0 {
				tbl.WithPagination(pageFlag, limitFlag)
			}

			summary := summarizeStatuses(res.Repos)

			for _, s := range res.Repos {
				repoCell := table.PlainCell(s.Name)
				pathCell := table.PlainCell(s.Path)

				switch s.Error {
				case "":
					branchCell := table.PlainCell(s.CurrentBranch)
					var syncCell table.Cell
					switch {
					case s.AheadCount > 0 && s.BehindCount > 0:
						syncCell = table.StyleCell(fmt.Sprintf("↕%d/%d diverged", s.AheadCount, s.BehindCount), warningStyle)
					case s.AheadCount > 0:
						syncCell = table.StyleCell(fmt.Sprintf("↑%d ahead", s.AheadCount), subtleStyle)
					case s.BehindCount > 0:
						syncCell = table.StyleCell(fmt.Sprintf("↓%d behind", s.BehindCount), subtleStyle)
					default:
						syncCell = table.StyleCell("up to date", subtleStyle)
					}

					var treeCell table.Cell
					if s.IsClean {
						treeCell = table.StyleCell("✔ clean", successStyle)
					} else {
						treeCell = table.StyleCell(fmt.Sprintf("✖ dirty (%d)", s.ModifiedCount), errorStyle)
					}

					tbl.AddStyledRow(repoCell, pathCell, branchCell, syncCell, treeCell)
				case workspacedomain.ErrMissing:
					tbl.AddStyledRow(repoCell, pathCell, table.StyleCell("-", subtleStyle), table.StyleCell("-", subtleStyle), table.StyleCell("not cloned", subtleStyle))
				case workspacedomain.ErrGitless:
					tbl.AddStyledRow(repoCell, pathCell, table.StyleCell("-", subtleStyle), table.StyleCell("-", subtleStyle), table.StyleCell("gitless — run orbit repair", warningStyle))
				default:
					tbl.AddStyledRow(repoCell, pathCell, table.StyleCell("-", subtleStyle), table.StyleCell("-", subtleStyle), table.StyleCell(s.Error, errorStyle))
				}
			}

			fmt.Fprintln(out)
			_ = tbl.Render(out)

			// Summary footer
			fmt.Fprintf(out, "\n%s  %s  %s  %s  %s\n",
				infoStyle.Render(fmt.Sprintf("Total: %d", summary.Total)),
				successStyle.Render(fmt.Sprintf("✔ %d clean", summary.Clean)),
				warningStyle.Render(fmt.Sprintf("⚠ %d dirty", summary.Dirty)),
				subtleStyle.Render(fmt.Sprintf("○ %d not cloned", summary.Missing)),
				warningStyle.Render(fmt.Sprintf("✖ %d gitless", summary.Gitless)),
			)
			if summary.OtherErrors > 0 {
				fmt.Fprintf(out, "%s\n", errorStyle.Render(fmt.Sprintf("  %d other errors", summary.OtherErrors)))
			}
			if summary.Gitless > 0 {
				fmt.Fprintf(out, "  %s  %s\n", iconInfo, subtleStyle.Render("gitless trees: orbit repair [scope]  (copies .git only; never checkout -f)"))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum rows per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
