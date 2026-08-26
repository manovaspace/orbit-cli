package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/session"
	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

var (
	version = "v0.1.0"
	commit  = "none"
	date    = "unknown"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		if (version == "dev" || version == "" || version == "v0.1.0") && info.Main.Version != "" && info.Main.Version != "(devel)" {
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
		Use:   "orbit",
		Short: "Orbit developer platform and workspace orchestrator",
		Long:  "Orbit developer onboarding, multi-repo sync, and dev stack orchestrator. (Shortcut: 'o')",
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
						boldStyle.Render("orbit onboard --resume"),
					)
				}
			}

			// Skip casual update check if disabled by env
			if os.Getenv("ORBIT_SKIP_UPDATE_CHECK") == "true" || os.Getenv("ORBIT_SKIP_UPDATE_CHECK") == "1" ||
				os.Getenv("MANOVA_SKIP_UPDATE_CHECK") == "true" || os.Getenv("MANOVA_SKIP_UPDATE_CHECK") == "1" {
				return
			}

			// Casual edge update check (1-hour TTL, fast Cloudflare edge GET)
			if checkRes, err := updater.CheckEdgeUpdateCasual(version, "", 1*time.Hour, ""); err == nil && checkRes != nil {
				if checkRes.HasUpdate {
					var hl []string
					if checkRes.Release != nil && checkRes.Release.ReleaseNotes != "" {
						hl = strings.Split(checkRes.Release.ReleaseNotes, "\n")
					}
					fmt.Fprintln(cmd.OutOrStdout(), renderUpdateBanner(version, checkRes.LatestVersion, hl))
				}
			}
		},
	}

	cmd.AddGroup(&cobra.Group{ID: "core", Title: "Core Commands:"})
	cmd.AddGroup(&cobra.Group{ID: "workspace", Title: "Workspace Commands:"})
	cmd.AddGroup(&cobra.Group{ID: "system", Title: "System & Tooling:"})

	onboardCmd := newOnboardCmd()
	onboardCmd.GroupID = "core"

	initCmd := newInitCmd()
	initCmd.GroupID = "core"

	doctorCmd := newDoctorCmd()
	doctorCmd.GroupID = "core"

	devCmd := newDevCmd()
	devCmd.GroupID = "core"

	statusCmd := newStatusCmd()
	statusCmd.GroupID = "workspace"

	syncCmd := newSyncCmd()
	syncCmd.GroupID = "workspace"

	updateCmd := newUpdateCmd()
	updateCmd.GroupID = "workspace"

	portCmd := newPortCmd()
	portCmd.GroupID = "workspace"

	envCmd := newEnvCmd()
	envCmd.GroupID = "workspace"

	migrateCmd := newMigrateCmd()
	migrateCmd.GroupID = "workspace"

	inviteCmd := newInviteCmd()
	inviteCmd.GroupID = "system"

	userCmd := newUserCmd()
	userCmd.GroupID = "system"

	docCmd := newDocCmd()
	docCmd.GroupID = "system"

	changelogCmd := newChangelogCmd()
	changelogCmd.GroupID = "system"

	selfUpdateCmd := newSelfUpdateCmd()
	selfUpdateCmd.GroupID = "system"

	uninstallCmd := newUninstallCmd()
	uninstallCmd.GroupID = "system"

	versionCmd := &cobra.Command{
		Use:     "version",
		GroupID: "system",
		Short:   "Print the CLI version and build metadata",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "orbit version %s (commit: %s, date: %s)\n", version, commit, date)
		},
	}

	// Register all subcommands
	cmd.AddCommand(onboardCmd)
	cmd.AddCommand(initCmd)
	cmd.AddCommand(doctorCmd)
	cmd.AddCommand(devCmd)
	cmd.AddCommand(statusCmd)
	cmd.AddCommand(syncCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(portCmd)
	cmd.AddCommand(envCmd)
	cmd.AddCommand(migrateCmd)
	cmd.AddCommand(inviteCmd)
	cmd.AddCommand(userCmd)
	cmd.AddCommand(docCmd)
	cmd.AddCommand(changelogCmd)
	cmd.AddCommand(selfUpdateCmd)
	cmd.AddCommand(uninstallCmd)
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
