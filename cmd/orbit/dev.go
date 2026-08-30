package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// ServiceStatus represents the connectivity and health state of a core local service.
type ServiceStatus struct {
	Name    string
	Host    string
	Port    int
	URL     string
	Ready   bool
	Message string
}

func findOrbitInfraDir(workspaceRoot string) string {
	if envDir := os.Getenv("ORBIT_INFRA_DIR"); envDir != "" {
		if fi, err := os.Stat(envDir); err == nil && fi.IsDir() {
			return envDir
		}
	}

	candidates := []string{
		filepath.Join(workspaceRoot, "orbit", "orbit-infra"),
		filepath.Join(workspaceRoot, "orbit-infra"),
		workspaceRoot,
	}

	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "compose.sh")); err == nil {
			return c
		}
		if _, err := os.Stat(filepath.Join(c, "core", "docker-compose.yml")); err == nil {
			return c
		}
	}

	return filepath.Join(workspaceRoot, "orbit", "orbit-infra")
}

type infraRunnerFunc func(dir string, stdout, stderr io.Writer, cmdName string, args ...string) error

var defaultInfraRunner = func(dir string, stdout, stderr io.Writer, cmdName string, args ...string) error {
	cmd := exec.Command(cmdName, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()

	return cmd.Run()
}

var infraRunner = defaultInfraRunner

func runInInfra(cmdName string, args ...string) error {
	return runInInfraWithIO(os.Stdout, os.Stderr, cmdName, args...)
}

func runInInfraWithIO(out, errOut io.Writer, cmdName string, args ...string) error {
	workspaceRoot := findWorkspaceRoot("")
	infraDir := findOrbitInfraDir(workspaceRoot)

	if fi, err := os.Stat(infraDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("orbit-infra directory not found at %s. Ensure orbit-infra is cloned", infraDir)
	}

	return infraRunner(infraDir, out, errOut, cmdName, args...)
}

// checkPortReady verifies if a TCP port is currently open and accepting connections.
func checkPortReady(host string, port int, timeout time.Duration) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// waitForServiceHealth repeatedly checks a TCP port until responsive or timeout/cancellation occurs.
func waitForServiceHealth(ctx context.Context, host string, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}

	for {
		if ctx.Err() != nil {
			return false
		}

		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return true
		}

		if time.Now().After(deadline) {
			return false
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// probeServiceReadiness probes core services in parallel and returns their status.
func probeServiceReadiness(ctx context.Context, timeout time.Duration) []ServiceStatus {
	coreServices := []struct {
		Name string
		Host string
		Port int
		URL  string
	}{
		{Name: "Postgres", Host: "127.0.0.1", Port: 10332, URL: "postgres://orbit@localhost:10332/orbit"},
		{Name: "NATS", Host: "127.0.0.1", Port: 10482, URL: "nats://localhost:10482"},
		{Name: "LLDAP", Host: "127.0.0.1", Port: 10389, URL: "ldap://localhost:10389"},
		{Name: "Caddy Ingress", Host: "127.0.0.1", Port: 10000, URL: "http://localhost:10000"},
	}

	results := make([]ServiceStatus, len(coreServices))
	var wg sync.WaitGroup

	for i, svc := range coreServices {
		wg.Add(1)
		go func(idx int, s struct {
			Name string
			Host string
			Port int
			URL  string
		}) {
			defer wg.Done()
			ready := waitForServiceHealth(ctx, s.Host, s.Port, timeout)
			msg := "Ready"
			if !ready {
				msg = "Unresponsive / Offline"
			}
			results[idx] = ServiceStatus{
				Name:    s.Name,
				Host:    s.Host,
				Port:    s.Port,
				URL:     s.URL,
				Ready:   ready,
				Message: msg,
			}
		}(i, svc)
	}

	wg.Wait()
	return results
}

// renderEndpointsBanner renders a styled Lipgloss card listing dev portals and service readiness.
func renderEndpointsBanner(out io.Writer, statuses []ServiceStatus) {
	var sb strings.Builder

	sb.WriteString(boldStyle.Render("Orbit Local Developer Endpoints") + "\n\n")

	endpoints := []struct {
		Name string
		URL  string
	}{
		{Name: "Developer Portal", URL: "http://localhost:10007"},
		{Name: "Authelia SSO", URL: "http://auth.dev.manova.space:10000"},
		{Name: "Forgejo Git", URL: "http://git.dev.manova.space:10000"},
		{Name: "Mailpit", URL: "http://mail.dev.manova.space:10000"},
		{Name: "Grafana", URL: "http://grafana.dev.manova.space:10000"},
	}

	for _, ep := range endpoints {
		sb.WriteString(fmt.Sprintf("  %s %-18s %s\n", iconArrow, boldStyle.Render(ep.Name+":"), subtleStyle.Render(ep.URL)))
	}

	if len(statuses) > 0 {
		sb.WriteString("\n" + boldStyle.Render("Service Readiness:") + "\n")
		for _, s := range statuses {
			icon := iconOK
			statusText := successStyle.Render("READY")
			if !s.Ready {
				icon = iconWarn
				statusText = warningStyle.Render("NOT READY")
			}
			sb.WriteString(fmt.Sprintf("  %s  %-16s %-12s (:%d)\n", icon, s.Name, statusText, s.Port))
		}
	}

	fmt.Fprintln(out, cardStyle.Render(sb.String()))
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
	var waitFlag bool
	var timeoutFlag time.Duration

	cmd := &cobra.Command{
		Use:   "up [modules...]",
		Short: "Start local development stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			fmt.Fprintln(out, titleStyle.Render("Starting Orbit Dev Stack..."))

			startScript := filepath.Join(infraDir, "scripts", "start-dev-stack.sh")
			var err error
			if _, statErr := os.Stat(startScript); statErr == nil {
				err = runInInfraWithIO(out, errOut, startScript, args...)
			} else {
				composeScript := filepath.Join(infraDir, "compose.sh")
				if _, statErr := os.Stat(composeScript); statErr == nil {
					err = runInInfraWithIO(out, errOut, composeScript, append([]string{"up", "-d"}, args...)...)
				} else {
					err = runInInfraWithIO(out, errOut, "docker", append([]string{"compose", "up", "-d"}, args...)...)
				}
			}

			if err != nil {
				return err
			}

			var statuses []ServiceStatus
			if waitFlag {
				fmt.Fprintln(out, infoStyle.Render(fmt.Sprintf("Probing service readiness (timeout: %s)...", timeoutFlag)))
				statuses = probeServiceReadiness(cmd.Context(), timeoutFlag)
			} else {
				statuses = probeServiceReadiness(cmd.Context(), 500*time.Millisecond)
			}

			renderEndpointsBanner(out, statuses)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&waitFlag, "wait", "w", false, "Wait for core services to become responsive")
	cmd.Flags().DurationVar(&timeoutFlag, "timeout", 15*time.Second, "Timeout when waiting for service readiness")

	return cmd
}

func newDevDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop local development stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			fmt.Fprintln(out, titleStyle.Render("Stopping Orbit Dev Stack..."))

			composeScript := filepath.Join(infraDir, "compose.sh")
			if _, err := os.Stat(composeScript); err == nil {
				return runInInfraWithIO(out, errOut, composeScript, "down")
			}

			return runInInfraWithIO(out, errOut, "docker", "compose", "down")
		},
	}
}

func newDevTier2Cmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tier2",
		Short: "Start Tier 2 Orbit services (orbit-auth, orbit-notifications, orbit-api-gateway)",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			fmt.Fprintln(out, titleStyle.Render("Starting Orbit Tier 2 Services..."))

			tier2Script := filepath.Join(infraDir, "scripts", "start-tier2.sh")
			if _, err := os.Stat(tier2Script); err == nil {
				return runInInfraWithIO(out, errOut, tier2Script, args...)
			}

			return fmt.Errorf("start-tier2.sh script not found at %s", tier2Script)
		},
	}
}

func newDevCaddyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "caddy [reload|restart|logs]",
		Short: "Manage local Caddy reverse proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			action := "reload"
			if len(args) > 0 {
				action = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			switch action {
			case "reload":
				fmt.Fprintln(out, titleStyle.Render("Reloading Caddy configuration..."))
				composeScript := filepath.Join(infraDir, "compose.sh")
				if _, err := os.Stat(composeScript); err == nil {
					return runInInfraWithIO(out, errOut, composeScript, "exec", "caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile")
				}
				return runInInfraWithIO(out, errOut, "docker", "compose", "exec", "caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile")
			case "restart":
				fmt.Fprintln(out, titleStyle.Render("Restarting Caddy container..."))
				return runInInfraWithIO(out, errOut, "docker", "compose", "restart", "caddy")
			case "logs":
				return runInInfraWithIO(out, errOut, "docker", "compose", "logs", "-f", "caddy")
			default:
				return fmt.Errorf("unknown caddy action %q. Valid actions: reload, restart, logs", action)
			}
		},
	}
}

func newDevPortalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "portal",
		Short: "Open or print the local developer portal URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			portalURL := "http://localhost:10007"

			fmt.Fprintln(out, titleStyle.Render("Orbit Local Developer Portal"))
			fmt.Fprintf(out, "  %s  Portal URL:     %s\n", iconOK, boldStyle.Render(portalURL))
			fmt.Fprintf(out, "  %s  Authelia SSO:   %s\n", iconInfo, subtleStyle.Render("http://auth.dev.manova.space:10000"))
			fmt.Fprintf(out, "  %s  Forgejo Git:    %s\n", iconInfo, subtleStyle.Render("http://git.dev.manova.space:10000"))
			fmt.Fprintf(out, "  %s  Mailpit:        %s\n", iconInfo, subtleStyle.Render("http://mail.dev.manova.space:10000"))
			fmt.Fprintf(out, "  %s  Grafana:        %s\n", iconInfo, subtleStyle.Render("http://grafana.dev.manova.space:10000"))

			return nil
		},
	}
}

func newDevLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs [service]",
		Short: "Stream logs from local dev stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			cmdArgs := []string{"compose", "logs", "-f"}
			if len(args) > 0 {
				cmdArgs = append(cmdArgs, args...)
			}
			return runInInfraWithIO(out, errOut, "docker", cmdArgs...)
		},
	}
}
