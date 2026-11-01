package main

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/dev/application"
	"github.com/manovaspace/orbit-cli/pkg/dev/infrastructure/infracli"
	"github.com/manovaspace/orbit-cli/pkg/dev/infrastructure/netprobe"
	devosfs "github.com/manovaspace/orbit-cli/pkg/dev/infrastructure/osfs"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/spf13/cobra"
)

// The dev commands reach the infrastructure checkout, docker and TCP probes
// through these seams.
var (
	devRunner runner.Runner     = runner.Exec{}
	devFiles  application.Files = devosfs.New()
	devProbe  application.Probe = netprobe.New()
)

func newDevService(out, errOut io.Writer) *application.Service {
	return &application.Service{
		Commands: infracli.New(devRunner, out, errOut),
		Files:    devFiles,
		Probe:    devProbe,
		Out:      out,
		ErrOut:   errOut,
		UI: application.UI{
			OK:    iconOK,
			Warn:  iconWarn,
			Info:  iconInfo,
			Arrow: iconArrow,

			Title:    func(text string) string { return titleStyle.Render(text) },
			Bold:     func(text string) string { return boldStyle.Render(text) },
			Subtle:   func(text string) string { return subtleStyle.Render(text) },
			Success:  func(text string) string { return successStyle.Render(text) },
			Warning:  func(text string) string { return warningStyle.Render(text) },
			InfoText: func(text string) string { return infoStyle.Render(text) },
			Card:     func(text string) string { return cardStyle.Render(text) },
		},
	}
}

func writeDevFailure(out io.Writer, jsonFlag bool, err error) {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
}

// findOrbitInfraDir is kept for onboard, which launches the stack directly.
func findOrbitInfraDir(workspaceRoot string) string {
	return devFiles.InfraDir(workspaceRoot)
}

// runInInfra is kept for onboard. It streams the process to the terminal.
func runInInfra(cmdName string, args ...string) error {
	infraDir := findOrbitInfraDir(findWorkspaceRoot(""))
	return infracli.New(devRunner, os.Stdout, os.Stderr).Stream(context.Background(), infraDir, cmdName, args...)
}

func newDevCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Control and orchestrate local development stacks and containers",
		Long:  "Executes Docker Compose and orchestration scripts located in orbit/orbit-infra to start/stop local databases, queues, identity services, and developer portals.",
	}

	cmd.AddCommand(newDevUpCmd())
	cmd.AddCommand(newDevDownCmd())
	cmd.AddCommand(newDevTier2Cmd())
	cmd.AddCommand(newDevCaddyCmd())
	cmd.AddCommand(newDevPortalCmd())
	cmd.AddCommand(newDevLogsCmd())

	return cmd
}

func newDevUpCmd() *cobra.Command {
	var (
		waitFlag bool
		timeout  time.Duration
		jsonFlag bool
	)

	cmd := &cobra.Command{
		Use:   "up [modules...]",
		Short: "Start local development stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			errOut := cmd.ErrOrStderr()
			if jsonFlag {
				uiOut = io.Discard
				errOut = io.Discard
			}

			res, err := newDevService(uiOut, errOut).Up(cmd.Context(), application.UpInput{
				WorkspaceRoot: findWorkspaceRoot(""),
				Modules:       args,
				Wait:          waitFlag,
				Timeout:       timeout,
			})
			if err != nil {
				writeDevFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&waitFlag, "wait", "w", false, "Wait for core services to become responsive")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "Timeout when waiting for service readiness")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

func newDevDownCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop local development stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			errOut := cmd.ErrOrStderr()
			if jsonFlag {
				uiOut = io.Discard
				errOut = io.Discard
			}

			res, err := newDevService(uiOut, errOut).Down(cmd.Context(), findWorkspaceRoot(""))
			if err != nil {
				writeDevFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newDevTier2Cmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "tier2",
		Short: "Start Tier 2 Orbit services (orbit-auth, orbit-notifications, orbit-api-gateway)",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			errOut := cmd.ErrOrStderr()
			if jsonFlag {
				uiOut = io.Discard
				errOut = io.Discard
			}

			res, err := newDevService(uiOut, errOut).Tier2(cmd.Context(), findWorkspaceRoot(""), args)
			if err != nil {
				writeDevFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newDevCaddyCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "caddy [reload|restart|logs]",
		Short: "Manage local Caddy reverse proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			errOut := cmd.ErrOrStderr()
			if jsonFlag {
				uiOut = io.Discard
				errOut = io.Discard
			}
			action := "reload"
			if len(args) > 0 {
				action = args[0]
			}

			res, err := newDevService(uiOut, errOut).Caddy(cmd.Context(), findWorkspaceRoot(""), action)
			if err != nil {
				writeDevFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newDevPortalCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "portal",
		Short: "Open or print the local developer portal URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			res := newDevService(uiOut, cmd.ErrOrStderr()).Portal(cmd.Context())
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newDevLogsCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Stream logs from local dev stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			errOut := cmd.ErrOrStderr()
			if jsonFlag {
				uiOut = io.Discard
				errOut = io.Discard
			}

			// JSON cannot carry an unbounded stream, so --json reports the
			// command that would run instead of starting it.
			res, err := newDevService(uiOut, errOut).Logs(cmd.Context(), application.LogsInput{
				WorkspaceRoot: findWorkspaceRoot(""),
				Services:      args,
				Plan:          jsonFlag,
			})
			if err != nil {
				writeDevFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}
