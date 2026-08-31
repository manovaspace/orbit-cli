package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

func newSelfUpdateCmd() *cobra.Command {
	var (
		checkOnly bool
		yesFlag   bool
	)

	cmd := &cobra.Command{
		Use:     "self-update",
		Aliases: []string{"selfupdate"},
		Short:   "Update the Orbit CLI binary to the latest release",
		Long:    "Checks GitHub releases for the latest Orbit CLI binary, displays release highlights, and performs atomic binary replacement.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			curVer := "v" + strings.TrimPrefix(version, "v")
			fmt.Fprintln(out, titleStyle.Render("Orbit CLI Self-Update"))

			apiURL := os.Getenv("ORBIT_RELEASE_API_URL")
			res, err := updater.CheckUpdate(version, apiURL, "")
			if err != nil {
				return fmt.Errorf("failed to check for updates: %w", err)
			}

			latestVer := "v" + strings.TrimPrefix(res.LatestVersion, "v")

			if !res.HasUpdate {
				fmt.Fprintf(out, "  %s  %s (Current: %s)\n",
					iconOK,
					successStyle.Render("Orbit CLI is already up to date!"),
					codeStyle.Render(curVer),
				)
				return nil
			}

			execPath, _ := os.Executable()
			if execPath == "" {
				execPath = "/usr/local/bin/orbit"
			}

			fmt.Fprintf(out, "  %s  %s %s %s\n",
				iconInfo,
				codeStyle.Render(curVer),
				iconArrow,
				successStyle.Render(latestVer),
			)

			// Display top 5 release highlights if present
			if res.Release != nil && res.Release.ReleaseNotes != "" {
				highlights := updater.ExtractReleaseHighlights(res.Release.ReleaseNotes, 5)
				if len(highlights) > 0 {
					fmt.Fprintf(out, "\n  %s\n", boldStyle.Render("Highlights in "+latestVer+":"))
					for _, h := range highlights {
						fmt.Fprintf(out, "    %s %s\n", subtleStyle.Render("•"), h)
					}
				}
			}
			fmt.Fprintln(out)

			if checkOnly {
				fmt.Fprintf(out, "  Run '%s' to install.\n", boldStyle.Render("orbit self-update"))
				return nil
			}

			if !yesFlag {
				if !promptConfirm(in, out, fmt.Sprintf("Update Orbit to %s?", latestVer), true) {
					fmt.Fprintf(out, "\n  %s  Update cancelled.\n", iconInfo)
					return nil
				}
			}

			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("Downloading and installing update..."))

			if err := updater.SelfUpdate(version, "", nil); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
					return fmt.Errorf("insufficient permissions to replace %s. Try running with sudo: sudo orbit self-update", execPath)
				}
				return fmt.Errorf("self-update failed: %w", err)
			}

			fmt.Fprintf(out, "  %s  %s\n", iconOK, successStyle.Render(fmt.Sprintf("Successfully updated Orbit to %s!", latestVer)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for updates without downloading or installing")
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Automatically accept update confirmation prompt")

	return cmd
}
