package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/session"
	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		if (version == "dev" || version == "") && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "none" && s.Value != "" {
					if len(s.Value) > 7 {
						commit = s.Value[:7]
					} else {
						commit = s.Value
					}
				}
			case "vcs.time":
				if date == "unknown" && s.Value != "" {
					date = s.Value
				}
			}
		}
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manova",
		Short: "Zero-leak developer onboarding and workspace orchestrator",
		Long:  "Fast, zero-leak developer onboarding, multi-repo synchronization, and dev stack orchestrator.",
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

			// Skip passive update check for dev builds, disabled env, and update/help commands
			if version == "dev" || os.Getenv("MANOVA_SKIP_UPDATE_CHECK") == "true" || cmdName == "version" || cmdName == "self-update" || cmdName == "selfupdate" || cmdName == "help" {
				return
			}

			// Non-blocking passive check with 24-hour TTL cache
			if cached, err := updater.CheckUpdateCached(version, "", 24*time.Hour, "", ""); err == nil && cached != nil && cached.HasUpdate {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s %s (%s %s %s). Run '%s' to upgrade.\n",
					iconInfo,
					warningStyle.Render("A newer version of manova CLI is available"),
					updater.FormatVersion(version),
					iconArrow,
					updater.FormatVersion(cached.LatestVersion),
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
	cmd.AddCommand(newUninstallCmd())
	cmd.AddCommand(newWorkerCmd())
	cmd.AddCommand(versionCmd)

	return cmd
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
