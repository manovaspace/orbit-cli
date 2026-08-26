package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/alias"
	"github.com/manovaspace/orbit-cli/pkg/manpage"
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
		Short:   "Uninstall CLI binary and clean local state",
		Long: `Uninstall the orbit CLI binary and remove local session checkpoints,
diagnostic caches (~/.orbit), and optionally purge cloned workspace repositories.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			fmt.Fprintln(out, titleStyle.Render("Orbit CLI Uninstaller"))

			if !opts.yes {
				promptMsg := "Are you sure you want to uninstall orbit CLI and remove all local configurations?"
				if !promptYesNo(in, out, promptMsg, false) {
					fmt.Fprintln(out, "Uninstallation cancelled.")
					return nil
				}
			}

			// 1. Remove Configuration, Session & Cache Directories (~/.orbit, ~/.config/orbit, and legacy)
			home, _ := os.UserHomeDir()
			if home != "" {
				configDirs := []string{
					filepath.Join(home, ".orbit"),
					filepath.Join(home, ".config", "orbit"),
					filepath.Join(home, ".manova"),
					filepath.Join(home, ".config", "manova"),
				}
				if !opts.keepConfig {
					for _, d := range configDirs {
						if _, err := os.Stat(d); err == nil {
							if err := os.RemoveAll(d); err != nil {
								fmt.Fprintf(out, "  %s  Failed to remove %s: %v\n", iconWarn, d, err)
							} else {
								fmt.Fprintf(out, "  %s  Removed configuration directory: %s\n", iconOK, subtleStyle.Render(d))
							}
						}
					}
					// Clean shell alias and autocompletion entries from RC files
					alias.RemoveShellConfiguration()
				} else {
					// Remove edge check cache and active session even when keeping general user config
					for _, d := range configDirs {
						_ = os.Remove(filepath.Join(d, "edge-version.json"))
						_ = os.Remove(filepath.Join(d, "session.json"))
					}
				}
			}

			// 2. Optional: Purge Cloned Workspace
			if opts.purgeWorkspace && home != "" {
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
			if home != "" {
				commonPaths := []string{
					filepath.Join(home, ".local", "bin", "orbit"),
					"/usr/local/bin/orbit",
					filepath.Join(home, "go", "bin", "orbit"),
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
			}

			// 4. Remove Unix Man Pages
			_ = manpage.UninstallManPages()
			fmt.Fprintf(out, "  %s  Removed Unix man pages\n", iconOK)

			fmt.Fprintf(out, "\n%s  %s\n", iconOK, successStyle.Render("Successfully uninstalled orbit CLI."))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip interactive confirmation prompt")
	cmd.Flags().BoolVar(&opts.yes, "force", false, "Alias for --yes")
	cmd.Flags().BoolVar(&opts.purgeWorkspace, "purge-workspace", false, "Also remove cloned workspace repository (~/Dev/Manova)")
	cmd.Flags().BoolVar(&opts.keepConfig, "keep-config", false, "Keep ~/.orbit configuration and session files")

	return cmd
}
