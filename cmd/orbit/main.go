package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/manovaspace/orbit-cli/pkg/session"
	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

var (
	version = "v0.5.2"
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
		Use:               "orbit",
		Short:             "Orbit developer platform and workspace orchestrator",
		Long:              "Orbit developer onboarding, multi-repo sync, and dev stack orchestrator. (Shortcut: 'o')",
		DisableAutoGenTag: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			return enforceHost(c, host.Live)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if shouldSuppressPostRunNotices(cmd) {
				return
			}

			// Check for pending onboarding session and notify user
			if sm, err := session.NewSessionManager(""); err == nil && sm.HasPendingSession() {
				if sess, err := sm.LoadSession(); err == nil && sess != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "\n%s %s (stage: %s).\n   Run '%s' to resume setup, or '%s' to discard.\n",
						iconInfo,
						infoStyle.Render("Ongoing onboarding session detected"),
						warningStyle.Render(string(sess.CurrentStage)),
						boldStyle.Render("orbit onboard --resume"),
						boldStyle.Render("orbit onboard --ignore-and-remove-checkpoint"),
					)
				}
			}

			// Check for available updates in background cache
			updater.NotifyIfUpdateAvailable(cmd.OutOrStdout(), version)
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version and build metadata",
		Run: func(cmd *cobra.Command, args []string) {
			out := cmd.OutOrStdout()
			var meta []string
			if commit != "" && commit != "none" {
				meta = append(meta, fmt.Sprintf("commit: %s", commit))
			}
			if date != "" && date != "unknown" {
				meta = append(meta, fmt.Sprintf("built: %s", date))
			}
			if len(meta) > 0 {
				fmt.Fprintf(out, "orbit version %s (%s)\n", version, strings.Join(meta, ", "))
			} else {
				fmt.Fprintf(out, "orbit version %s\n", version)
			}
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
	cmd.AddCommand(newUninstallCmd())
	cmd.AddCommand(newDocCmd())
	cmd.AddCommand(newInviteCmd())
	cmd.AddCommand(newOnboardCmd())
	cmd.AddCommand(newAdminCmd())
	cmd.AddCommand(newStaffCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newAssetsCmd())
	cmd.AddCommand(newCompletionCmd())
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
		"init":        true,
		"onboard":     true,
		"version":     true,
		"self-update": true,
		"selfupdate":  true,
		"doc":         true,
		"help":        true,
		"completion":  true,
		"config":      true,
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
