package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

func newSelfUpdateCmd() *cobra.Command {
	var (
		checkOnly     bool
		targetVersion string
	)

	cmd := &cobra.Command{
		Use:     "self-update",
		Aliases: []string{"selfupdate"},
		Short:   "Update CLI binary to latest release",
		Long:    "Checks Forgejo/GitHub releases for the latest manova CLI binary, downloads matching OS/arch archive, and replaces the running binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			fmt.Fprintln(out, titleStyle.Render("Manova CLI Self-Update"))
			fmt.Fprintf(out, "  Current Version: %s\n\n", codeStyle.Render(updater.FormatVersion(version)))

			// If target version is explicitly requested:
			if targetVersion != "" {
				if !strings.HasPrefix(targetVersion, "v") && !strings.Contains(targetVersion, ".") {
					targetVersion = "v" + targetVersion
				}
				fmt.Fprintf(out, "Targeting specific release: %s\n\n", boldStyle.Render(targetVersion))
				if checkOnly {
					fmt.Fprintf(out, "Run 'manova self-update --version %s' without --check to install.\n", targetVersion)
					return nil
				}

				fmt.Fprintf(out, "%s\n", headerStyle.Render("Downloading and installing update..."))
				if err := updater.SelfUpdateTo(version, targetVersion, "", nil); err != nil {
					return fmt.Errorf("self-update failed: %w", err)
				}

				_ = os.Remove(updater.ExpandCachePath(""))
				fmt.Fprintf(out, "  %s  %s\n", iconOK, successStyle.Render(fmt.Sprintf("Successfully updated manova to %s!", targetVersion)))

				execPath, _ := os.Executable()
				postCtx := &migrate.PostUpdateContext{
					Interactive: true,
					In:          in,
					Out:         out,
					ExecPath:    execPath,
					PrevVersion: version,
					NewVersion:  targetVersion,
				}
				if migResults, err := migrate.RunPostUpdateMigrations(postCtx); err == nil {
					for _, r := range migResults {
						if r.Success && !r.Skipped {
							fmt.Fprintf(out, "  %s  %s\n", iconOK, subtleStyle.Render(r.Description))
						}
					}
				}
				return nil
			}

			res, err := updater.CheckUpdate(version, "", "")
			if err != nil {
				return fmt.Errorf("failed to check for updates: %w", err)
			}

			if !res.HasUpdate {
				fmt.Fprintf(out, "  %s  %s (Latest: %s)\n",
					iconOK,
					successStyle.Render("manova CLI is already up to date!"),
					codeStyle.Render(updater.FormatVersion(res.LatestVersion)),
				)
				return nil
			}

			fmt.Fprintf(out, "  %s  New release available: %s %s %s\n",
				iconInfo,
				codeStyle.Render(updater.FormatVersion(version)),
				iconArrow,
				successStyle.Render(updater.FormatVersion(res.LatestVersion)),
			)

			// Show Top 5 Release Notes
			if res.Release != nil && res.Release.ReleaseNotes != "" {
				highlights := updater.TruncateReleaseNotes(res.Release.ReleaseNotes, 5)
				if len(highlights) > 0 {
					fmt.Fprintf(out, "\n  %s\n", headerStyle.Render("Release Highlights:"))
					for _, h := range highlights {
						if strings.HasPrefix(h, "…") {
							fmt.Fprintf(out, "    %s\n", subtleStyle.Render(h))
						} else {
							fmt.Fprintf(out, "    • %s\n", updater.FormatTerminalHighlight(h))
						}
					}
				}
			}

			if checkOnly {
				fmt.Fprintln(out, "\n  Run 'manova self-update' without --check to install.")
				return nil
			}

			fmt.Fprintf(out, "\n%s\n", headerStyle.Render("Downloading and installing update..."))

			if err := updater.SelfUpdate(version, "", nil); err != nil {
				return fmt.Errorf("self-update failed: %w", err)
			}

			// Clean cached update check so next command runs against latest state
			_ = os.Remove(updater.ExpandCachePath(""))

			fmt.Fprintf(out, "  %s  %s\n", iconOK, successStyle.Render(fmt.Sprintf("Successfully updated manova to %s!", updater.FormatVersion(res.LatestVersion))))

			// Run Post-Update Environment Migrations (systemd worker, completions, m alias prompt)
			execPath, _ := os.Executable()
			postCtx := &migrate.PostUpdateContext{
				Interactive: true,
				In:          in,
				Out:         out,
				ExecPath:    execPath,
				PrevVersion: version,
				NewVersion:  res.LatestVersion,
			}
			if migResults, err := migrate.RunPostUpdateMigrations(postCtx); err == nil {
				for _, r := range migResults {
					if r.Success && !r.Skipped {
						fmt.Fprintf(out, "  %s  %s\n", iconOK, subtleStyle.Render(r.Description))
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for updates without downloading or installing")
	cmd.Flags().StringVarP(&targetVersion, "version", "v", "", "Target specific release version (e.g. v0.2.8)")

	return cmd
}
