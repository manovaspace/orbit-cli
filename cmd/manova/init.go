package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/alias"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/manovaspace/orbit-cli/pkg/worker"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		manifestFlag  string
		concurrency   int
		skipHooks     bool
		bootstrapFlag bool
	)

	cmd := &cobra.Command{
		Use:   "init [scope]",
		Short: "Clone and initialize workspace repositories or bootstrap environment",
		Long: `Resolves repository targets from workspace.yaml and clones them concurrently.
Scopes:
  core            - Clones essential core baseline (default)
  all / *         - Clones all repositories in the manifest
  orbit           - Clones Orbit platform toolkit
  manovaspace     - Clones open-source commons
  clients/<name>  - Clones a specific client cluster
  <repo-name>     - Clones an individual repository`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			scope := "core"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}

			// Root check: Suggest dedicated non-root developer user
			if os.Geteuid() == 0 {
				fmt.Fprintf(out, "\n  %s  %s\n", iconWarn, warningStyle.Render("Running as root is not recommended for development workspaces."))
				if promptYesNo(in, out, "Would you like to create a dedicated developer user with sudo privileges?", true) {
					username := promptString(in, out, "Developer username", "dev")
					if err := doctor.CreateDevUser(cmd.Context(), username, out); err != nil {
						return fmt.Errorf("failed to create developer user: %w", err)
					}
					fmt.Fprintf(out, "\n  %s  %s\n\n", iconOK, successStyle.Render(fmt.Sprintf("Developer user '%s' is ready. To switch: su - %s", username, username)))
				}
			}

			// Pre-flight: verify mandatory Zsh, Oh My Zsh and default login shell environment
			zshRes := doctor.CheckZsh()
			omzRes := doctor.CheckOhMyZsh()
			shellRes := doctor.CheckDefaultShell()
			if zshRes.Status != doctor.StatusOK || omzRes.Status != doctor.StatusOK || shellRes.Status != doctor.StatusOK {
				fmt.Fprintf(out, "\n  %s  %s\n", iconWarn, warningStyle.Render("Zsh, Oh My Zsh, and Zsh default shell are required for Manova developer workspaces."))
				if promptYesNo(in, out, "Would you like to install and configure Zsh + Oh My Zsh now?", true) {
					_ = doctor.WarmUpSudo(cmd.Context(), in, out)
					pm := doctor.DetectPackageManager()
					if zshRes.Status != doctor.StatusOK {
						if err := doctor.InstallZsh(cmd.Context(), pm, out); err != nil {
							return fmt.Errorf("failed to install Zsh: %w", err)
						}
					}
					if omzRes.Status != doctor.StatusOK {
						if err := doctor.InstallOhMyZsh(cmd.Context(), out); err != nil {
							return fmt.Errorf("failed to install Oh My Zsh: %w", err)
						}
					}
					if shellRes.Status != doctor.StatusOK {
						if err := doctor.SetDefaultShellZsh(cmd.Context(), out); err != nil {
							return fmt.Errorf("failed to set default shell: %w", err)
						}
					}
					if home, err := os.UserHomeDir(); err == nil {
						_ = doctor.EnsureZshConfigured(home)
					}
					_, _ = alias.InstallShellCompletion(true)
					_, _ = alias.AddShellAlias("m", "manova")
				} else {
					return fmt.Errorf("zsh and oh-my-zsh are mandatory for Manova workspace development; setup aborted")
				}
			} else {
				if home, err := os.UserHomeDir(); err == nil {
					_ = doctor.EnsureZshConfigured(home)
				}
				_, _ = alias.InstallShellCompletion(true)
				_, _ = alias.AddShellAlias("m", "manova")
			}

			// Auto-start background edge worker
			execPath, _ := os.Executable()
			_, _ = worker.StartDaemon(execPath)

			if bootstrapFlag {
				fmt.Fprintf(out, "\n%s\n", successStyle.Render("✔ Manova developer environment initialized!"))
				fmt.Fprintf(out, "  • Zsh and Oh My Zsh configured as default shell.\n")
				fmt.Fprintf(out, "  • 'm' alias and shell completions active in ~/.zshrc.\n")
				fmt.Fprintf(out, "  • Background update worker pulse active.\n\n")
				fmt.Fprintf(out, "When you receive an invitation token, run '%s' to claim your workspace.\n\n", boldStyle.Render("m onboard"))
				return nil
			}

			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, manifestFlag)

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Manova Workspace Init — Scope: %s", scope)))
			fmt.Fprintf(out, "  Workspace Root: %s\n", subtleStyle.Render(workspaceRoot))
			fmt.Fprintf(out, "  Manifest Path:  %s\n\n", subtleStyle.Render(manifestPath))

			if _, err := os.Stat(manifestPath); err != nil {
				return fmt.Errorf("manifest file not found at %s. Ensure workspace.yaml exists or pass --manifest", manifestPath)
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return fmt.Errorf("failed to load workspace manifest: %w", err)
			}

			targets, err := m.ResolveRepos(scope)
			if err != nil {
				return fmt.Errorf("failed to resolve repositories for scope %q: %w", scope, err)
			}

			if len(targets) == 0 {
				fmt.Fprintf(out, "No repositories found for scope %q.\n", scope)
				return nil
			}

			fmt.Fprintf(out, "%s\n", headerStyle.Render(fmt.Sprintf("Cloning %d repositories (concurrency: %d)...", len(targets), concurrency)))

			clonedCount := 0
			existCount := 0
			failCount := 0

			results := orchestrator.CloneTargets(workspaceRoot, targets, concurrency, func(res orchestrator.CloneResult) {
				if res.Success {
					if res.AlreadyExists {
						fmt.Fprintf(out, "  %s  %s %s\n", iconOK, boldStyle.Render(res.Name), subtleStyle.Render("(already exists)"))
					} else {
						fmt.Fprintf(out, "  %s  %s %s\n", iconOK, boldStyle.Render(res.Name), successStyle.Render("(cloned)"))
					}
				} else {
					fmt.Fprintf(out, "  %s  %s %s\n", iconError, boldStyle.Render(res.Name), errorStyle.Render(fmt.Sprintf("(failed: %s)", res.Error)))
				}
			})

			for _, res := range results {
				if res.Success {
					if res.AlreadyExists {
						existCount++
					} else {
						clonedCount++
					}
				} else {
					failCount++
				}
			}

			// Post-clone hooks & migrations
			if !skipHooks {
				fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── Post-Clone Setup & Workspace Migrations ─────────────────"))
				migResults, err := migrate.RunPendingMigrations(workspaceRoot)
				if err != nil {
					fmt.Fprintf(out, "  %s  Workspace migration warning: %v\n", iconWarn, err)
				} else {
					appliedCount := 0
					for _, mr := range migResults {
						if mr.Success {
							appliedCount++
							fmt.Fprintf(out, "  %s  Migration applied: %s (%s)\n", iconOK, boldStyle.Render(mr.ID), mr.Description)
						}
					}
					if appliedCount == 0 {
						fmt.Fprintf(out, "  %s  Workspace directory structure and hooks are up to date.\n", iconOK)
					}
				}
			}

			// Summary
			fmt.Fprintf(out, "\n%s  %s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d cloned", clonedCount)),
				infoStyle.Render(fmt.Sprintf("ℹ %d already present", existCount)),
				errorStyle.Render(fmt.Sprintf("✖ %d failed", failCount)),
			)

			if failCount > 0 {
				return fmt.Errorf("clone completed with %d error(s)", failCount)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent clone workers")
	cmd.Flags().BoolVar(&skipHooks, "skip-hooks", false, "Skip post-clone workspace hooks and migrations")
	cmd.Flags().BoolVar(&bootstrapFlag, "bootstrap", false, "Bootstrap base developer environment without cloning workspace manifest")

	return cmd
}
