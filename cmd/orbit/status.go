package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
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
		OK:        render.IconOK,
		Warn:      render.IconWarn,
		Error:     render.IconError,
		Info:      render.IconInfo,
		Title:     func(text string) string { return render.TitleLegacy.Render(text) },
		Header:    func(text string) string { return render.Header.Render(text) },
		Bold:      func(text string) string { return render.Bold.Render(text) },
		Success:   func(text string) string { return render.Success.Render(text) },
		Warning:   func(text string) string { return render.Warning.Render(text) },
		ErrorText: func(text string) string { return render.Error.Render(text) },
		InfoText:  func(text string) string { return render.Info.Render(text) },
		Subtle:    func(text string) string { return render.Subtle.Render(text) },
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
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			res, err := newWorkspaceService(io.Discard).Status(cmd.Context(), workspaceapp.StatusInput{
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

			p := render.NewPrinter(out)
			renderStatusResult(p, scope, res.Repos, pageFlag, limitFlag)
			return p.Err()
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum rows per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

// renderStatusResult writes the text form from the same repository statuses the
// JSON envelope carries.
func renderStatusResult(p *render.Printer, scope string, repos []workspacedomain.RepoStatus, page, limit int) {
	p.Title(fmt.Sprintf("Workspace status — %s", scope))

	summary := summarizeStatuses(repos)
	if len(repos) > 0 {
		tbl := table.New(
			table.Column{Title: "REPOSITORY", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 16},
			table.Column{Title: "PATH", HeaderStyle: render.Header, CellStyle: render.Subtle, MinWidth: 20, Flexible: true},
			table.Column{Title: "BRANCH", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 8},
			table.Column{Title: "SYNC", HeaderStyle: render.Header, MinWidth: 12},
			table.Column{Title: "WORKING TREE", HeaderStyle: render.Header, MinWidth: 14},
		)
		if limit > 0 {
			tbl.WithPagination(page, limit)
		}

		for _, s := range repos {
			branch := "-"
			syncText := "-"
			tree := ""

			switch s.Error {
			case "":
				branch = s.CurrentBranch
				switch {
				case s.AheadCount > 0 && s.BehindCount > 0:
					syncText = fmt.Sprintf("%s ↕%d/%d diverged", render.StatusWarn.Icon(), s.AheadCount, s.BehindCount)
				case s.AheadCount > 0:
					syncText = fmt.Sprintf("%s ↑%d ahead", render.StatusInfo.Icon(), s.AheadCount)
				case s.BehindCount > 0:
					syncText = fmt.Sprintf("%s ↓%d behind", render.StatusInfo.Icon(), s.BehindCount)
				default:
					syncText = render.StatusInfo.Icon() + " up to date"
				}
				if s.IsClean {
					tree = render.StatusOK.Icon() + " clean"
				} else {
					tree = fmt.Sprintf("%s dirty (%d)", render.StatusWarn.Icon(), s.ModifiedCount)
				}
			case workspacedomain.ErrMissing:
				tree = render.StatusSkipped.Icon() + " not cloned"
			case workspacedomain.ErrGitless:
				tree = render.StatusFail.Icon() + " gitless — run orbit repair"
			default:
				tree = render.StatusFail.Icon() + " " + s.Error
			}

			tbl.AddRow(s.Name, s.Path, branch, syncText, tree)
		}

		_ = p.Table(tbl)

		if summary.Gitless > 0 {
			p.Line(render.StatusInfo, "gitless trees: orbit repair [scope] (copies .git only; never checkout -f)")
		}
	} else {
		p.Empty(render.Plural(0, "repository", "repositories"))
	}

	p.Summary(
		render.SummaryPart{Status: render.StatusInfo, Count: summary.Total, Label: render.Plural(summary.Total, "repository", "repositories")},
		render.SummaryPart{Status: render.StatusOK, Count: summary.Clean, Label: "clean"},
		render.SummaryPart{Status: render.StatusWarn, Count: summary.Dirty, Label: "dirty"},
		render.SummaryPart{Status: render.StatusSkipped, Count: summary.Missing, Label: "not cloned"},
		render.SummaryPart{Status: render.StatusFail, Count: summary.Gitless, Label: "gitless"},
		render.SummaryPart{Status: render.StatusFail, Count: summary.OtherErrors, Label: render.Plural(summary.OtherErrors, "other error", "other errors")},
	)
}
