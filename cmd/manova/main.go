package main

import (
	"fmt"
	"os"
	"time"

	"git.dev.manova.space/manova/orbit-cli/pkg/session"
	"git.dev.manova.space/manova/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manova",
		Short: "Manova Workspace Orchestrator & Developer Tooling",
		Long: `Manova CLI (manova) orchestrates multi-repo operations across the Manova workspace.
Supports workspace initialization, system diagnostics, branch synchronization,
schema-driven environment management, 50-port block allocations, and container orchestration.`,
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			cmdName := cmd.Name()

			// Check for pending onboarding session and notify user (only in human-readable output)
			if cmdName != "onboard" && cmdName != "version" && cmdName != "self-update" && cmdName != "selfupdate" && cmdName != "help" {
				jsonFlag, _ := cmd.Flags().GetBool("json")
				formatFlag, _ := cmd.Flags().GetString("format")
				if !jsonFlag && formatFlag != "json" {
					if sm, err := session.NewSessionManager(""); err == nil && sm.HasPendingSession() {
						if sess, err := sm.LoadSession(); err == nil && sess != nil {
							fmt.Fprintf(cmd.OutOrStdout(), "\n%s %s (stage: %s).\n   Run '%s' to resume setup.\n",
								iconInfo,
								infoStyle.Render("Ongoing onboarding session detected"),
								warningStyle.Render(string(sess.CurrentStage)),
								boldStyle.Render("manova onboard --resume"),
							)
						}
					}
				}
			}

			// Skip passive update check for version and self-update commands
			if cmdName == "version" || cmdName == "self-update" || cmdName == "selfupdate" || cmdName == "help" {
				return
			}

			// Non-blocking passive check with 24-hour TTL cache
			if cached, err := updater.CheckUpdateCached(version, "", 24*time.Hour, "", ""); err == nil && cached != nil && cached.HasUpdate {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s %s (v%s %s v%s). Run '%s' to upgrade.\n",
					iconInfo,
					warningStyle.Render("A newer version of manova CLI is available"),
					version,
					iconArrow,
					cached.LatestVersion,
					boldStyle.Render("manova self-update"),
				)
			}
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version and build metadata",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "manova version %s (commit: %s, date: %s)\n", version, commit, date)
		},
	}

	// Register all subcommands
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newEnvCmd())
	cmd.AddCommand(newPortCmd())
	cmd.AddCommand(newMigrateCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newDevCmd())
	cmd.AddCommand(newSelfUpdateCmd())
	cmd.AddCommand(newInviteCmd())
	cmd.AddCommand(newOnboardCmd())
	cmd.AddCommand(versionCmd)

	return cmd
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
