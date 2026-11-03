package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/sessionstore"
	"github.com/manovaspace/orbit-cli/pkg/output"
	ownerfilestore "github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/filestore"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/updater"
	"github.com/spf13/cobra"
)

var (
	version = "v0.9.5"
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

// rootCmdFactory builds the command tree. The root command uses it so tests can
// register a fixture command and drive the real run entry point.
var rootCmdFactory = newRootCmd

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "orbit",
		Short:             "Orbit developer platform and workspace orchestrator",
		Long:              "Orbit developer onboarding, multi-repo sync, and dev stack orchestrator. (Shortcut: 'o')",
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		SilenceErrors:     true,
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

			// Check for pending onboarding session and notify user (only if not verified as platform owner/admin)
			ownerStore := ownerfilestore.NewStore("")
			if !ownerStore.IsVerified() {
				if sm, err := sessionstore.New(""); err == nil && sm.HasPending() {
					if sess, err := sm.Load(); err == nil && sess != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "\n%s %s (stage: %s).\n   Run '%s' to resume setup, or '%s' to discard.\n",
							iconInfo,
							infoStyle.Render("Ongoing onboarding session detected"),
							warningStyle.Render(string(sess.CurrentStage)),
							boldStyle.Render("orbit onboard --resume"),
							boldStyle.Render("orbit onboard --ignore-and-remove-checkpoint"),
						)
					}
				}
			}

			// Check for available updates in background cache
			updater.NotifyIfUpdateAvailable(cmd.ErrOrStderr(), version)
		},
	}

	cmd.PersistentFlags().StringVar(&configFlag, "config", "", "Custom path to Orbit CLI configuration file")

	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return clierr.Usage(err.Error())
	})

	var versionJSON bool
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version and build metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if versionJSON {
				return render.Emit(out, true, versionInfo{
					Version: version,
					Commit:  knownValue(commit, "none"),
					Built:   knownValue(date, "unknown"),
				}, nil)
			}
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
			return nil
		},
	}
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "Print the result as a JSON envelope")

	// Register all subcommands
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newRepairCmd())
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
	cmd.AddCommand(newPRCmd())
	cmd.AddCommand(newCICmd())
	cmd.AddCommand(newAuditCmd())
	cmd.AddCommand(newReleaseCmd())
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
		"doctor":      true,
		"env":         true,
		"status":      true,
		"port":        true,
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
		"pr":          true,
		"ci":          true,
		"audit":       true,
		"release":     true,
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
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one CLI invocation and reports exactly one result. In text mode
// a failure is one "Error: ..." line plus an optional hint on stderr; in --json
// mode it is exactly one envelope on stdout (ADR-047 D5/D7).
func run(args []string, stdout, stderr io.Writer) int {
	output.ResetWritten()

	cmd := rootCmdFactory()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	executed, err := cmd.ExecuteC()
	if err == nil {
		return clierr.ExitOK
	}

	err = asUsageError(err)
	if jsonRequested(args) {
		if !output.Written() {
			_ = output.Write(stdout, output.Failure(err))
		}
		return clierr.ExitCode(err)
	}

	fmt.Fprintf(stderr, "Error: %s\n", err)
	if hint := errorHint(err); hint != "" {
		fmt.Fprintf(stderr, "  hint: %s\n", hint)
	}
	if clierr.Code(err) == clierr.CodeUsage && executed != nil {
		fmt.Fprintf(stderr, "Run '%s --help' for usage.\n", executed.CommandPath())
	}
	return clierr.ExitCode(err)
}

// asUsageError classifies cobra's own argument and flag errors as usage errors.
// Errors that already carry a clierr code keep their class.
func asUsageError(err error) error {
	if err == nil {
		return nil
	}
	var c *clierr.Error
	if errors.As(err, &c) {
		return err
	}
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"flag needs an argument",
		"invalid argument",
		"required flag(s)",
		"accepts ",
		"requires at least",
		"requires at most",
		"unknown help topic",
	} {
		if strings.HasPrefix(msg, prefix) {
			return clierr.Usage(msg)
		}
	}
	return err
}

// jsonRequested reports whether --json appears before a literal "--".
func jsonRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
}

func errorHint(err error) string {
	var c *clierr.Error
	if errors.As(err, &c) {
		return c.Hint
	}
	return ""
}

// versionInfo is the --json data shape of the version command.
type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Built   string `json:"built"`
}

// knownValue maps a build placeholder to the empty string.
func knownValue(value, unknown string) string {
	if value == "" || value == unknown {
		return ""
	}
	return value
}

// exitCodeFor maps a command error to the process exit code. Errors that do not
// carry a clierr code still exit 1, so existing commands keep their behaviour.
func exitCodeFor(err error) int {
	return clierr.ExitCode(err)
}
