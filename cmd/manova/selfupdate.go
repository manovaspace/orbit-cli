package main

import (
	"fmt"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

func newSelfUpdateCmd() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:     "self-update",
		Aliases: []string{"selfupdate"},
		Short:   "Update the manova CLI binary to the latest release",
		Long:    "Checks GitHub releases for the latest manova CLI binary, downloads matching OS/arch archive, and replaces the running binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			curVer := "v" + strings.TrimPrefix(version, "v")
			fmt.Fprintln(out, titleStyle.Render("Manova CLI Self-Update"))
			fmt.Fprintf(out, "  Current Version: %s\n\n", codeStyle.Render(curVer))

			res, err := updater.CheckUpdate(version, "", "")
			if err != nil {
				return fmt.Errorf("failed to check for updates: %w", err)
			}

			latestVer := "v" + strings.TrimPrefix(res.LatestVersion, "v")

			if !res.HasUpdate {
				fmt.Fprintf(out, "  %s  %s (Latest: %s)\n",
					iconOK,
					successStyle.Render("Orbit CLI is already up to date!"),
					codeStyle.Render(latestVer),
				)
				return nil
			}

			fmt.Fprintf(out, "  %s  New release available: %s %s %s\n",
				iconInfo,
				codeStyle.Render(curVer),
				iconArrow,
				successStyle.Render(latestVer),
			)

			if checkOnly {
				fmt.Fprintln(out, "\n  Run 'orbit self-update' without --check to install.")
				return nil
			}

			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("Downloading and installing update..."))

			if err := updater.SelfUpdate(version, "", nil); err != nil {
				return fmt.Errorf("self-update failed: %w", err)
			}

			fmt.Fprintf(out, "  %s  %s\n", iconOK, successStyle.Render(fmt.Sprintf("Successfully updated Orbit to %s!", latestVer)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for updates without downloading or installing")

	return cmd
}
