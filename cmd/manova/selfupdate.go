package main

import (
	"fmt"

	"git.dev.manova.space/manova/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

func newSelfUpdateCmd() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:     "self-update",
		Aliases: []string{"selfupdate"},
		Short:   "Update the manova CLI binary to the latest release",
		Long:    "Checks Forgejo/GitHub releases for the latest manova CLI binary, downloads matching OS/arch archive, and replaces the running binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Manova CLI Self-Update"))
			fmt.Fprintf(out, "  Current Version: %s\n\n", codeStyle.Render("v"+version))

			res, err := updater.CheckUpdate(version, "", "")
			if err != nil {
				return fmt.Errorf("failed to check for updates: %w", err)
			}

			if !res.HasUpdate {
				fmt.Fprintf(out, "  %s  %s (Latest: %s)\n",
					iconOK,
					successStyle.Render("manova CLI is already up to date!"),
					codeStyle.Render("v"+res.LatestVersion),
				)
				return nil
			}

			fmt.Fprintf(out, "  %s  New release available: %s %s %s\n",
				iconInfo,
				codeStyle.Render("v"+version),
				iconArrow,
				successStyle.Render("v"+res.LatestVersion),
			)

			if checkOnly {
				fmt.Fprintln(out, "\n  Run 'manova self-update' without --check to install.")
				return nil
			}

			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("Downloading and installing update..."))

			if err := updater.SelfUpdate(version, "", nil); err != nil {
				return fmt.Errorf("self-update failed: %w", err)
			}

			fmt.Fprintf(out, "  %s  %s\n", iconOK, successStyle.Render(fmt.Sprintf("Successfully updated manova to v%s!", res.LatestVersion)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for updates without downloading or installing")

	return cmd
}
