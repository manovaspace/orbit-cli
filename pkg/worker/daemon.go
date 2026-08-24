package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// resolveExecPath resolves the binary executable path with fallback to "manova".
func resolveExecPath(override string) string {
	if override != "" {
		return override
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "manova"
}

// GetServiceUnitContent generates the systemd user service unit definition.
func GetServiceUnitContent(execPath string) string {
	execPath = resolveExecPath(execPath)
	return fmt.Sprintf(`[Unit]
Description=Manova Edge Version Worker
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s worker run-once
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`, execPath)
}

// GetTimerUnitContent generates the systemd user timer unit definition.
func GetTimerUnitContent() string {
	return `[Unit]
Description=Trigger Manova Edge Version Worker every 1 minute

[Timer]
OnBootSec=30s
OnUnitActiveSec=1m
Unit=manova-worker.service

[Install]
WantedBy=timers.target
`
}

// InstallSystemdUnits writes the service and timer files to ~/.config/systemd/user
// and enables the timer unit via systemctl --user.
func InstallSystemdUnits(execPath string) error {
	servicePath := ExpandPath(DefaultServiceUnitPath)
	timerPath := ExpandPath(DefaultTimerUnitPath)

	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		return fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	if err := os.WriteFile(servicePath, []byte(GetServiceUnitContent(execPath)), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", servicePath, err)
	}

	if err := os.WriteFile(timerPath, []byte(GetTimerUnitContent()), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", timerPath, err)
	}

	cmdReload := exec.Command("systemctl", "--user", "daemon-reload")
	if out, err := cmdReload.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	cmdEnable := exec.Command("systemctl", "--user", "enable", "--now", "manova-worker.timer")
	if out, err := cmdEnable.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable --now manova-worker.timer failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// RemoveSystemdUnits stops and disables the systemd user timer and removes the unit files.
func RemoveSystemdUnits() error {
	if IsSystemdFunctional() {
		cmdDisable := exec.Command("systemctl", "--user", "disable", "--now", "manova-worker.timer")
		_ = cmdDisable.Run()
	}

	servicePath := ExpandPath(DefaultServiceUnitPath)
	timerPath := ExpandPath(DefaultTimerUnitPath)

	_ = os.Remove(servicePath)
	_ = os.Remove(timerPath)

	if IsSystemdFunctional() {
		cmdReload := exec.Command("systemctl", "--user", "daemon-reload")
		_ = cmdReload.Run()
	}

	return nil
}

// IsSystemdFunctional checks if systemctl --user is usable in the current environment.
func IsSystemdFunctional() bool {
	if os.Getenv("MANOVA_FORCE_DETACHED") == "1" || os.Getenv("MANOVA_FORCE_DETACHED") == "true" {
		return false
	}
	if os.Getenv("MANOVA_FORCE_SYSTEMD") == "1" || os.Getenv("MANOVA_FORCE_SYSTEMD") == "true" {
		return true
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", "--user", "list-units", "--no-pager")
	err := cmd.Run()
	return err == nil
}

// WritePID writes the worker PID to the specified file.
func WritePID(pidFile string, pid int) error {
	expanded := ExpandPath(pidFile)
	if err := os.MkdirAll(filepath.Dir(expanded), 0755); err != nil {
		return fmt.Errorf("failed to create directory for pid file: %w", err)
	}
	return os.WriteFile(expanded, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

// ReadPID reads the worker PID from the specified file.
func ReadPID(pidFile string) (int, error) {
	expanded := ExpandPath(pidFile)
	data, err := os.ReadFile(expanded)
	if err != nil {
		return 0, err
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file %q: %w", expanded, err)
	}
	return pid, nil
}

// RemovePID removes the PID file.
func RemovePID(pidFile string) error {
	expanded := ExpandPath(pidFile)
	return os.Remove(expanded)
}

// IsProcessAlive checks whether a process with the given PID is actively running.
func IsProcessAlive(pid int) bool {
	return isProcessAlive(pid)
}

// StartDaemon starts the worker daemon. If systemd user sessions are supported,
// it installs and enables the systemd timer. Otherwise, it launches a detached background process
// and records its PID in ~/.manova/worker.pid.
func StartDaemon(execPath string) (string, error) {
	execPath = resolveExecPath(execPath)

	if IsSystemdFunctional() {
		if err := InstallSystemdUnits(execPath); err != nil {
			return "", fmt.Errorf("failed to install systemd units: %w", err)
		}
		_ = RemovePID(DefaultPIDFile)

		if state, err := ReadState(DefaultStateFile); err == nil && state != nil {
			state.WorkerMode = "systemd"
			state.WorkerStatus = "running"
			state.WorkerPID = 0
			_ = WriteStateAtomic(DefaultStateFile, state)
		}

		return "systemd", nil
	}

	pidPath := ExpandPath(DefaultPIDFile)
	if pid, err := ReadPID(pidPath); err == nil && pid > 0 {
		if IsProcessAlive(pid) {
			return "detached", nil
		}
		_ = RemovePID(pidPath)
	}

	cmd := exec.Command(execPath, "worker", "run")
	setDetachedProcessAttr(cmd)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start detached worker process: %w", err)
	}

	pid := cmd.Process.Pid
	if err := WritePID(pidPath, pid); err != nil {
		return "detached", fmt.Errorf("process started (pid %d) but failed to write PID file: %w", pid, err)
	}

	if state, err := ReadState(DefaultStateFile); err == nil && state != nil {
		state.WorkerMode = "detached"
		state.WorkerStatus = "running"
		state.WorkerPID = pid
		_ = WriteStateAtomic(DefaultStateFile, state)
	}

	return "detached", nil
}

// StopDaemon stops the worker daemon by disabling systemd units or terminating the detached PID.
func StopDaemon() error {
	if IsSystemdFunctional() {
		_ = RemoveSystemdUnits()
	}

	pidPath := ExpandPath(DefaultPIDFile)
	if pid, err := ReadPID(pidPath); err == nil && pid > 0 {
		if IsProcessAlive(pid) {
			_ = killProcess(pid)
		}
		_ = RemovePID(pidPath)
	}

	if state, err := ReadState(DefaultStateFile); err == nil && state != nil {
		state.WorkerStatus = "stopped"
		state.WorkerPID = 0
		_ = WriteStateAtomic(DefaultStateFile, state)
	}

	return nil
}

// GetDaemonStatus retrieves the current operational status of the worker daemon.
func GetDaemonStatus() (*DaemonStatus, error) {
	status := &DaemonStatus{
		Mode:   "inactive",
		Active: false,
	}

	if state, err := ReadState(DefaultStateFile); err == nil && state != nil {
		status.LastCheckedAt = state.LastCheckedAt
		status.ServerStatus = state.ServerStatus
		status.LatestVersion = state.LatestVersion
		status.LastError = state.LastError
	}

	if IsSystemdFunctional() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "systemctl", "--user", "is-active", "manova-worker.timer").Output()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			status.Mode = "systemd"
			status.Active = true
			return status, nil
		}
	}

	pidPath := ExpandPath(DefaultPIDFile)
	if pid, err := ReadPID(pidPath); err == nil && pid > 0 {
		if IsProcessAlive(pid) {
			status.Mode = "detached"
			status.Active = true
			status.PID = pid
			return status, nil
		}
	}

	return status, nil
}

// RunDaemonLoop continuously polls the edge version endpoint every interval until context is cancelled.
func RunDaemonLoop(ctx context.Context, endpoint, statePath string, interval time.Duration) error {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	if endpoint == "" {
		endpoint = DefaultEdgeURL
	}
	if statePath == "" {
		statePath = DefaultStateFile
	}

	// Initial poll on startup
	_, _ = PollOnce(endpoint, statePath)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, _ = PollOnce(endpoint, statePath)
		}
	}
}
