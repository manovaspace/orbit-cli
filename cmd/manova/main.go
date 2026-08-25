package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/manovaspace/orbit-cli/pkg/notifier"
	"github.com/manovaspace/orbit-cli/pkg/session"
	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/manovaspace/orbit-cli/pkg/worker"
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
		Use:     "manova",
		Aliases: []string{"m"},
		Short:   "Zero-leak developer onboarding and workspace orchestrator",
		Long: `Fast, zero-leak developer onboarding, multi-repo synchronization, and dev stack orchestrator.

Shortcut Alias:
  'm' is configured as a fast shell alias for 'manova' (e.g. 'm status', 'm dev up', 'm onboard').`,
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if shouldSuppressPostRunNotices(cmd) {
				return
			}

			// Check for pending onboarding session and notify user (only in human-readable output)
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

			// Skip passive update/feed check if disabled by env
			if os.Getenv("MANOVA_SKIP_UPDATE_CHECK") == "true" || os.Getenv("MANOVA_SKIP_UPDATE_CHECK") == "1" {
				return
			}

			// Render active notifier messages from ~/.manova/feed.json
			hasFeedMessages := false
			if feedState, err := notifier.ReadFeedState(notifier.DefaultFeedFile); err == nil && feedState != nil {
				store, _ := notifier.ReadStore(notifier.DefaultStoreFile)
				if store == nil {
					store = &notifier.MessageStore{}
				}
				visible := notifier.FilterVisible(feedState.Messages, store)
				for _, msg := range visible {
					hasFeedMessages = true
					fmt.Fprintln(cmd.OutOrStdout(), renderMessageBanner(msg))
					_ = notifier.MarkSeen(notifier.DefaultStoreFile, msg.ID)
				}
			}

			// Check ~/.manova/edge-version.json in 0ms using watchdog
			needsHealing, state, _ := worker.CheckWatchdog(worker.DefaultStateFile, worker.DefaultStaleThreshold)
			if needsHealing {
				execPath, _ := os.Executable()
				worker.HealWorkerBackground(execPath)
			}

			// If no feed messages were displayed, fallback to legacy version banner if newer release detected
			if !hasFeedMessages && state != nil && updater.IsNewerVersion(version, state.LatestVersion) {
				fmt.Fprintln(cmd.OutOrStdout(), renderUpdateBanner(version, state.LatestVersion, state.Highlights))
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

func shouldSuppressPostRunNotices(cmd *cobra.Command) bool {
	if cmd == nil {
		return true
	}
	names := []string{cmd.Name()}
	for p := cmd.Parent(); p != nil; p = p.Parent() {
		names = append(names, p.Name())
	}
	suppressList := map[string]bool{
		"uninstall":   true,
		"remove":      true,
		"purge":       true,
		"onboard":     true,
		"version":     true,
		"self-update": true,
		"selfupdate":  true,
		"help":        true,
		"worker":      true,
		"completion":  true,
	}
	for _, n := range names {
		if suppressList[n] {
			return true
		}
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	formatFlag, _ := cmd.Flags().GetString("format")
	if jsonFlag || formatFlag == "json" {
		return true
	}

	return false
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
