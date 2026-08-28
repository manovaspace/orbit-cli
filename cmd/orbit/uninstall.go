package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	var (
		force          bool
		purgeState     bool
		purgeWorkspace bool
	)

	cmd := &cobra.Command{
		Use:     "uninstall",
		Aliases: []string{"remove", "purge"},
		Short:   "Uninstall Orbit CLI binaries and cleanup shell configuration",
		Long:    "Removes Orbit CLI binaries from system and user paths, cleans shell completions, and optionally purges local session/cache state.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			// Check for workspace context and uncommitted changes
			workspaceRoot := findWorkspaceRoot("")
			manifestPath := findManifestPath(workspaceRoot, "")
			var dirtyRepos []string
			if _, err := os.Stat(manifestPath); err == nil {
				if m, err := manifest.Load(manifestPath); err == nil {
					if targets, err := m.ResolveRepos("all"); err == nil && len(targets) > 0 {
						statuses := orchestrator.GetWorkspaceStatus(workspaceRoot, targets)
						for _, st := range statuses {
							if !st.IsClean && st.Error == "" {
								dirtyRepos = append(dirtyRepos, fmt.Sprintf("%s (%d uncommitted changes)", st.Name, st.ModifiedCount))
							}
						}
					}
				}
			}

			if len(dirtyRepos) > 0 {
				fmt.Fprintf(out, "\n  %s  %s\n", iconWarn, warningStyle.Render("Workspace Uncommitted Changes Detected:"))
				for _, dr := range dirtyRepos {
					fmt.Fprintf(out, "     • %s\n", dr)
				}
				if !purgeWorkspace {
					fmt.Fprintln(out, subtleStyle.Render("     (Note: Your source code repositories will remain intact on disk)"))
				}
				fmt.Fprintln(out)
			}

			if purgeWorkspace && len(dirtyRepos) > 0 && !force {
				return fmt.Errorf("cannot purge workspace: %d repositories have uncommitted changes. Commit or stash them first, or pass --force", len(dirtyRepos))
			}

			if !force && istty.IsInteractiveSession() {
				fmt.Fprintln(out, titleStyle.Render("Orbit CLI Uninstaller"))
				if !promptYesNo(os.Stdin, out, "Are you sure you want to uninstall Orbit CLI from this system?", false) {
					fmt.Fprintln(out, subtleStyle.Render("Uninstallation aborted."))
					return nil
				}
			}

			fmt.Fprintln(out, titleStyle.Render("Uninstalling Orbit CLI..."))

			home, _ := os.UserHomeDir()
			candidates := []string{
				"/usr/local/bin/orbit",
				"/usr/local/bin/o",
				"/usr/local/bin/manova",
				filepath.Join(home, ".local", "bin", "orbit"),
				filepath.Join(home, ".local", "bin", "o"),
				filepath.Join(home, ".local", "bin", "manova"),
			}

			if execPath, err := os.Executable(); err == nil {
				candidates = append(candidates, execPath)
			}

			seen := make(map[string]bool)
			removedCount := 0
			for _, path := range candidates {
				if path == "" || seen[path] {
					continue
				}
				seen[path] = true
				if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
					if err := removeFileElevated(path); err == nil {
						fmt.Fprintf(out, "  %s  Removed binary: %s\n", iconOK, subtleStyle.Render(path))
						removedCount++
					} else {
						fmt.Fprintf(out, "  %s  Failed to remove binary %s: %v\n", iconError, path, err)
					}
				}
			}

			// Clean shell completions
			completionCandidates := []string{
				"/etc/bash_completion.d/orbit",
				"/etc/bash_completion.d/manova",
				filepath.Join(home, ".zsh", "completions", "_orbit"),
				filepath.Join(home, ".zsh", "completions", "_manova"),
			}
			for _, cp := range completionCandidates {
				if _, err := os.Stat(cp); err == nil {
					_ = removeFileElevated(cp)
					fmt.Fprintf(out, "  %s  Removed shell completion: %s\n", iconOK, subtleStyle.Render(cp))
				}
			}

			// Purge state directories if requested
			if purgeState {
				stateDirs := []string{
					filepath.Join(home, ".orbit"),
					filepath.Join(home, ".manova"),
				}
				for _, sd := range stateDirs {
					if _, err := os.Stat(sd); err == nil {
						_ = os.RemoveAll(sd)
						fmt.Fprintf(out, "  %s  Purged workspace state cache: %s\n", iconOK, subtleStyle.Render(sd))
					}
				}
			}

			if removedCount == 0 {
				fmt.Fprintln(out, subtleStyle.Render("No active Orbit binaries found in standard locations."))
			} else {
				fmt.Fprintf(out, "\n  %s  %s\n\n", iconOK, successStyle.Render("Orbit CLI uninstalled successfully."))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "yes", "y", false, "Uninstall without confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Alias for --yes")
	cmd.Flags().BoolVar(&purgeState, "purge-state", false, "Purge local cache and session state (~/.orbit, ~/.manova)")
	cmd.Flags().BoolVar(&purgeWorkspace, "purge-workspace", false, "Purge workspace repositories (blocked if uncommitted changes exist)")

	return cmd
}

func removeFileElevated(target string) error {
	if err := os.Remove(target); err == nil {
		return nil
	}
	if os.Geteuid() != 0 {
		cmd := exec.Command("sudo", "rm", "-f", target)
		return cmd.Run()
	}
	return os.Remove(target)
}
