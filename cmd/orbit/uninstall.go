package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	uninstallapp "github.com/manovaspace/orbit-cli/pkg/uninstall/application"
	uninstallosfs "github.com/manovaspace/orbit-cli/pkg/uninstall/infrastructure/osfs"
	workspaceapp "github.com/manovaspace/orbit-cli/pkg/workspace/application"
	"github.com/spf13/cobra"
)

// uninstallFS is the seam through which uninstall removes files.
var uninstallFS uninstallapp.FS = uninstallosfs.New(runner.Exec{})

func uninstallUI() uninstallapp.UI {
	return uninstallapp.UI{
		OK:    iconOK,
		Warn:  iconWarn,
		Error: iconError,
		Info:  iconInfo,

		Title:     func(text string) string { return titleStyle.Render(text) },
		Success:   func(text string) string { return successStyle.Render(text) },
		Warning:   func(text string) string { return warningStyle.Render(text) },
		ErrorText: func(text string) string { return errorStyle.Render(text) },
		Subtle:    func(text string) string { return subtleStyle.Render(text) },
	}
}

func newUninstallCmd() *cobra.Command {
	var (
		force          bool
		purgeState     bool
		purgeWorkspace bool
		jsonFlag       bool
	)

	cmd := &cobra.Command{
		Use:     "uninstall",
		Aliases: []string{"remove", "purge"},
		Short:   "Uninstall Orbit CLI binaries and cleanup shell configuration",
		Long:    "Removes Orbit CLI binaries from system and user paths, cleans shell completions, and optionally purges cache, session, and the owner vault.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			// Check for workspace context and uncommitted changes
			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, "")
			var dirtyRepos []string
			statuses, statusErr := newWorkspaceService(io.Discard).Status(cmd.Context(), workspaceapp.StatusInput{
				WorkspaceRoot: workspaceRoot,
				ManifestPath:  manifestPath,
				Scope:         "all",
			})
			if statusErr == nil {
				for _, st := range statuses.Repos {
					if !st.IsClean && st.Error == "" {
						dirtyRepos = append(dirtyRepos, fmt.Sprintf("%s (%d uncommitted changes)", st.Name, st.ModifiedCount))
					}
				}
			}

			svc := &uninstallapp.Service{FS: uninstallFS, Out: uiOut, UI: uninstallUI()}
			res, err := svc.Uninstall(cmd.Context(), uninstallapp.Input{
				Force:          force,
				PurgeState:     purgeState,
				PurgeWorkspace: purgeWorkspace,
				Interactive:    !jsonFlag && istty.IsInteractiveSession(),
				DirtyRepos:     dirtyRepos,
				Confirm: func(question string, defaultYes bool) bool {
					return promptYesNo(cmd.InOrStdin(), uiOut, question, defaultYes)
				},
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "yes", "y", false, "Uninstall without confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Alias for --yes")
	cmd.Flags().BoolVar(&purgeState, "purge-state", false, "Purge cache, session, and owner vault (~/.orbit, ~/.config/orbit)")
	cmd.Flags().BoolVar(&purgeWorkspace, "purge-workspace", false, "Purge workspace repositories (blocked if uncommitted changes exist)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
