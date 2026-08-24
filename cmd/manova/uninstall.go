package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type uninstallOptions struct {
	yes            bool
	purgeWorkspace bool
	keepConfig     bool
}

func newUninstallCmd() *cobra.Command {
	opts := &uninstallOptions{}

	cmd := &cobra.Command{
		Use:     "uninstall",
		Aliases: []string{"remove", "purge"},
		Short:   "Completely remove manova CLI binary and local configurations",
		Long: `Uninstall the manova CLI binary and remove local session checkpoints,
diagnostic caches (~/.manova), and optionally purge cloned workspace repositories.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			fmt.Fprintln(out, titleStyle.Render("Manova CLI Uninstaller"))

			if !opts.yes {
				promptMsg := "Are you sure you want to uninstall manova CLI and remove all local configurations?"
				if !promptYesNo(in, out, promptMsg, false) {
					fmt.Fprintln(out, "Uninstallation cancelled.")
					return nil
				}
			}

			// 1. Remove Configuration & Cache Directory (~/.manova)
			if !opts.keepConfig {
				home, err := os.UserHomeDir()
				if err == nil {
					configDir := filepath.Join(home, ".manova")
					if _, err := os.Stat(configDir); err == nil {
						if err := os.RemoveAll(configDir); err != nil {
							fmt.Fprintf(out, "  %s  Failed to remove %s: %v\n", iconWarn, configDir, err)
						} else {
							fmt.Fprintf(out, "  %s  Removed configuration directory: %s\n", iconOK, subtleStyle.Render(configDir))
						}
					}
				}
			}

			// 2. Optional: Purge Cloned Workspace
			if opts.purgeWorkspace {
				home, _ := os.UserHomeDir()
				workspaceDir := filepath.Join(home, "Dev", "Manova")
				if _, err := os.Stat(workspaceDir); err == nil {
					if err := os.RemoveAll(workspaceDir); err != nil {
						fmt.Fprintf(out, "  %s  Failed to remove workspace %s: %v\n", iconWarn, workspaceDir, err)
					} else {
						fmt.Fprintf(out, "  %s  Removed workspace directory: %s\n", iconOK, subtleStyle.Render(workspaceDir))
					}
				}
			}

			// 3. Remove Binary Executable
			execPath, err := os.Executable()
			if err == nil {
				execPath, _ = filepath.EvalSymlinks(execPath)
				if err := os.Remove(execPath); err != nil {
					fmt.Fprintf(out, "  %s  Failed to remove active binary (%s): %v\n", iconWarn, execPath, err)
					fmt.Fprintf(out, "     Try running: sudo rm -f %s\n", execPath)
				} else {
					fmt.Fprintf(out, "  %s  Removed CLI binary: %s\n", iconOK, subtleStyle.Render(execPath))
				}
			}

			// Check and clean known standard install paths
			home, _ := os.UserHomeDir()
			commonPaths := []string{
				filepath.Join(home, ".local", "bin", "manova"),
				"/usr/local/bin/manova",
				filepath.Join(home, "go", "bin", "manova"),
			}
			for _, p := range commonPaths {
				if p != execPath {
					if _, err := os.Stat(p); err == nil {
						if err := os.Remove(p); err == nil {
							fmt.Fprintf(out, "  %s  Removed binary: %s\n", iconOK, subtleStyle.Render(p))
						}
					}
				}
			}

			fmt.Fprintf(out, "\n%s  %s\n", iconOK, successStyle.Render("Successfully uninstalled manova CLI."))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation prompt")
	cmd.Flags().BoolVar(&opts.yes, "force", false, "Alias for --yes")
	cmd.Flags().BoolVar(&opts.purgeWorkspace, "purge-workspace", false, "Also remove cloned workspace repository (~/Dev/Manova)")
	cmd.Flags().BoolVar(&opts.keepConfig, "keep-config", false, "Keep ~/.manova configuration and session files")

	return cmd
}
