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
		Long:    "Checks GitHub releases for the latest Orbit CLI binary, displays release info with confirmation, and performs atomic binary replacement.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			curVer := "v" + strings.TrimPrefix(version, "v")
			fmt.Fprintln(out, titleStyle.Render("Orbit CLI Self-Update"))
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

			execPath, _ := os.Executable()
			if execPath == "" {
				execPath = "/usr/local/bin/orbit"
			}

			fmt.Fprintf(out, "  %s  %s\n\n",
				iconInfo,
				boldStyle.Render(fmt.Sprintf("New release available: %s %s %s", curVer, iconArrow, latestVer)),
			)

			updateSummary := fmt.Sprintf("  %-18s %s %s %s\n  %-18s %s\n  %-18s %s\n  %-18s %s",
				headerStyle.Render("Version:"), codeStyle.Render(curVer), iconArrow, successStyle.Render(latestVer),
				headerStyle.Render("Channel:"), subtleStyle.Render("GitHub Releases (manovaspace/orbit-cli)"),
				headerStyle.Render("Target Binary:"), subtleStyle.Render(execPath),
				headerStyle.Render("Release Notes:"), subtleStyle.Render(fmt.Sprintf("https://github.com/manovaspace/orbit-cli/releases/tag/%s", latestVer)),
			)
			fmt.Fprintln(out, renderCard("UPDATE DETAILS", updateSummary))
			fmt.Fprintln(out)

			if checkOnly {
				fmt.Fprintln(out, "  Run 'orbit self-update' without --check to apply this update.")
				return nil
			}

			if !yesFlag {
				if !promptConfirm(in, out, fmt.Sprintf("Proceed with updating Orbit to %s?", latestVer), true) {
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
