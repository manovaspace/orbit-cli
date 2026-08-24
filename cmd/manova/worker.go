package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/worker"
	"github.com/spf13/cobra"
)

func newWorkerCmd() *cobra.Command {
	var endpoint string
	var statePath string

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage the background edge version poller worker daemon",
		Long:  "Commands to start, stop, check status, and execute the background edge version worker daemon.",
	}

	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "Cloudflare edge version endpoint URL")
	cmd.PersistentFlags().StringVar(&statePath, "state", "", "Path to local edge version state JSON file")

	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerStopCmd())
	cmd.AddCommand(newWorkerStatusCmd(&statePath))
	cmd.AddCommand(newWorkerRunOnceCmd(&endpoint, &statePath))
	cmd.AddCommand(newWorkerRunCmd(&endpoint, &statePath))

	return cmd
}

func newWorkerStartCmd() *cobra.Command {
	var execPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the background worker daemon (systemd user timer or detached process)",
		Long:  "Start the background worker daemon. Installs and enables systemd user units if functional; otherwise spawns a detached background process.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Manova Worker Daemon — Start"))

			mode, err := worker.StartDaemon(execPath)
			if err != nil {
				return fmt.Errorf("failed to start worker daemon: %w", err)
			}

			if mode == "systemd" {
				fmt.Fprintf(out, "  %s %s (%s)\n",
					iconOK,
					successStyle.Render("Worker daemon started successfully"),
					infoStyle.Render("systemd user timer: manova-worker.timer"),
				)
			} else {
				fmt.Fprintf(out, "  %s %s (%s)\n",
					iconOK,
					successStyle.Render("Worker daemon started successfully"),
					infoStyle.Render("detached background process"),
				)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&execPath, "exec", "", "Path to manova executable for daemon configuration")

	return cmd
}

func newWorkerStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the background worker daemon",
		Long:  "Stop the background worker daemon. Disables and removes systemd user units and terminates any running detached worker processes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Manova Worker Daemon — Stop"))

			if err := worker.StopDaemon(); err != nil {
				return fmt.Errorf("failed to stop worker daemon: %w", err)
			}

			fmt.Fprintf(out, "  %s %s\n",
				iconOK,
				successStyle.Render("Worker daemon stopped successfully"),
			)

			return nil
		},
	}

	return cmd
}

func newWorkerStatusCmd(statePath *string) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of the worker daemon and edge connection",
		Long:  "Displays worker daemon liveness, polling frequency, last edge check time, and edge server health.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			daemonStatus, err := worker.GetDaemonStatus()
			if err != nil {
				return fmt.Errorf("failed to retrieve worker status: %w", err)
			}

			// If statePath was specifically overridden, read state from there
			if statePath != nil && *statePath != "" {
				if customState, err := worker.ReadState(*statePath); err == nil && customState != nil {
					daemonStatus.LastCheckedAt = customState.LastCheckedAt
					daemonStatus.ServerStatus = customState.ServerStatus
					daemonStatus.LatestVersion = customState.LatestVersion
					daemonStatus.LastError = customState.LastError
				}
			}

			if jsonOutput {
				data, err := json.MarshalIndent(daemonStatus, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON output: %w", err)
				}
				fmt.Fprintln(out, string(data))
				return nil
			}

			fmt.Fprintln(out, titleStyle.Render("Manova Background Worker Status"))

			var statusBadge string
			if daemonStatus.Active {
				if daemonStatus.Mode == "systemd" {
					statusBadge = successStyle.Render("● Active (systemd timer)")
				} else {
					statusBadge = successStyle.Render(fmt.Sprintf("● Active (detached, PID %d)", daemonStatus.PID))
				}
			} else {
				statusBadge = subtleStyle.Render("○ Inactive (stopped)")
			}

			var serverBadge string
			if daemonStatus.ServerStatus == "ok" {
				serverBadge = successStyle.Render("✔ Operational (200 OK)")
			} else if daemonStatus.ServerStatus == "down" {
				serverBadge = errorStyle.Render("✖ Outage / Degraded")
			} else {
				serverBadge = subtleStyle.Render("Unknown")
			}

			timeStr := "Never"
			if !daemonStatus.LastCheckedAt.IsZero() {
				timeStr = fmt.Sprintf("%s (%s ago)",
					daemonStatus.LastCheckedAt.Format(time.RFC3339),
					time.Since(daemonStatus.LastCheckedAt).Round(time.Second),
				)
			}

			versionStr := daemonStatus.LatestVersion
			if versionStr == "" {
				versionStr = "Unknown"
			}

			var cardContent strings.Builder
			cardContent.WriteString(fmt.Sprintf("  Worker Liveness:     %s\n", statusBadge))
			cardContent.WriteString(fmt.Sprintf("  Edge Server Health:  %s\n", serverBadge))
			cardContent.WriteString(fmt.Sprintf("  Latest Version:      %s\n", codeStyle.Render(versionStr)))
			cardContent.WriteString(fmt.Sprintf("  Last Edge Check:     %s\n", timeStr))
			if daemonStatus.LastError != "" {
				cardContent.WriteString(fmt.Sprintf("  Last Error:          %s\n", errorStyle.Render(daemonStatus.LastError)))
			}

			fmt.Fprintln(out, cardStyle.Render(cardContent.String()))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output status in JSON format")

	return cmd
}

func newWorkerRunOnceCmd(endpoint, statePath *string) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "run-once",
		Short: "Perform a single edge version poll and persist state",
		Long:  "Pings the Cloudflare edge /version endpoint once, logs status, and atomically writes the state file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			ep := ""
			if endpoint != nil {
				ep = *endpoint
			}
			sp := ""
			if statePath != nil {
				sp = *statePath
			}

			state, err := worker.PollOnce(ep, sp)

			if jsonOutput {
				data, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON output: %w", err)
				}
				fmt.Fprintln(out, string(data))
				return nil
			}

			if err != nil {
				fmt.Fprintf(out, "%s %s: %s\n",
					iconWarn,
					warningStyle.Render("Edge poll finished with warning/error"),
					err.Error(),
				)
			} else {
				fmt.Fprintf(out, "%s %s\n", iconOK, successStyle.Render("Edge version check completed successfully."))
			}

			fmt.Fprintf(out, "  Latest Version: %s\n", codeStyle.Render(state.LatestVersion))
			fmt.Fprintf(out, "  Server Status:  %s\n", state.ServerStatus)
			fmt.Fprintf(out, "  Last Checked:   %s\n", state.LastCheckedAt.Format(time.RFC3339))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output result in JSON format")

	return cmd
}

func newWorkerRunCmd(endpoint, statePath *string) *cobra.Command {
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run continuous background polling loop",
		Long:  "Executes a persistent loop polling the edge server every interval until interrupted.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ep := ""
			if endpoint != nil {
				ep = *endpoint
			}
			sp := ""
			if statePath != nil {
				sp = *statePath
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			err := worker.RunDaemonLoop(ctx, ep, sp, interval)
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", 1*time.Minute, "Polling interval duration")

	return cmd
}
