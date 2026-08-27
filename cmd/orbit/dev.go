package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

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

func runInInfra(cmdName string, args ...string) error {
	workspaceRoot := findWorkspaceRoot("")
	infraDir := findOrbitInfraDir(workspaceRoot)

	if fi, err := os.Stat(infraDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("orbit-infra directory not found at %s. Ensure orbit-infra is cloned", infraDir)
	}

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = infraDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	return cmd.Run()
}

func newDevUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up [modules...]",
		Short: "Start local development stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			fmt.Println(titleStyle.Render("Starting Manova Dev Stack..."))

			startScript := filepath.Join(infraDir, "scripts", "start-dev-stack.sh")
			if _, err := os.Stat(startScript); err == nil {
				return runInInfra(startScript, args...)
			}

			composeScript := filepath.Join(infraDir, "compose.sh")
			if _, err := os.Stat(composeScript); err == nil {
				return runInInfra(composeScript, append([]string{"up", "-d"}, args...)...)
			}

			return runInInfra("docker", append([]string{"compose", "up", "-d"}, args...)...)
		},
	}
}

func newDevDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop local development stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			fmt.Println(titleStyle.Render("Stopping Manova Dev Stack..."))

			composeScript := filepath.Join(infraDir, "compose.sh")
			if _, err := os.Stat(composeScript); err == nil {
				return runInInfra(composeScript, "down")
			}

			return runInInfra("docker", "compose", "down")
		},
	}
}

func newDevTier2Cmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tier2",
		Short: "Start Tier 2 Orbit services (orbit-auth, orbit-notifications, orbit-api-gateway)",
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			tier2Script := filepath.Join(infraDir, "scripts", "start-tier2.sh")
			if _, err := os.Stat(tier2Script); err == nil {
				return runInInfra(tier2Script, args...)
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
			action := "reload"
			if len(args) > 0 {
				action = args[0]
			}

			workspaceRoot := findWorkspaceRoot("")
			infraDir := findOrbitInfraDir(workspaceRoot)

			switch action {
			case "reload":
				fmt.Println(titleStyle.Render("Reloading Caddy configuration..."))
				composeScript := filepath.Join(infraDir, "compose.sh")
				if _, err := os.Stat(composeScript); err == nil {
					return runInInfra(composeScript, "exec", "caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile")
				}
				return runInInfra("docker", "compose", "exec", "caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile")
			case "restart":
				fmt.Println(titleStyle.Render("Restarting Caddy container..."))
				return runInInfra("docker", "compose", "restart", "caddy")
			case "logs":
				return runInInfra("docker", "compose", "logs", "-f", "caddy")
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

			fmt.Fprintln(out, titleStyle.Render("Manova Local Developer Portal"))
			fmt.Fprintf(out, "  %s  Portal URL: %s\n", iconOK, boldStyle.Render(portalURL))
			fmt.Fprintf(out, "  %s  Identity / Auth: %s\n", iconInfo, subtleStyle.Render("http://auth.dev.manova.space:10000"))
			fmt.Fprintf(out, "  %s  Forgejo Git:    %s\n", iconInfo, subtleStyle.Render("http://git.dev.manova.space:10000"))
			fmt.Fprintf(out, "  %s  Mailpit:        %s\n", iconInfo, subtleStyle.Render("http://mail.dev.manova.space:10000"))

			return nil
		},
	}
}

func newDevLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs [service]",
		Short: "Stream logs from local dev stack containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdArgs := []string{"compose", "logs", "-f"}
			if len(args) > 0 {
				cmdArgs = append(cmdArgs, args...)
			}
			return runInInfra("docker", cmdArgs...)
		},
	}
}
