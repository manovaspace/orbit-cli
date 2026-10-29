package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/ci"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/spf13/cobra"
)

func newCICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Switch the CI runner and sync GitHub Actions secrets",
		Long: "Move private repositories between the GitHub-hosted runner and the standby vps " +
			"runner, and push each repository's secrets without writing them to argv.",
	}
	cmd.AddCommand(newCIRunnerCmd())
	cmd.AddCommand(newCISecretsCmd())
	return cmd
}

func newCIRunnerCmd() *cobra.Command {
	var manifestFlag string
	cmd := &cobra.Command{
		Use:   "runner vps|github|status",
		Short: "Start or stop the standby runner and set CI_RUNNER on private repos",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := args[0]
			if mode != "vps" && mode != "github" && mode != "status" {
				return fmt.Errorf("unknown runner mode %q: use vps, github or status", mode)
			}
			m, err := loadCIManifest(manifestFlag)
			if err != nil {
				return err
			}
			repos, err := ci.PrivateRepos(m.AllRepos(), execCI)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if mode == "status" {
				status, err := ci.RunnerStatus(repos, execCI)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "gh-ci service: %s\n", status.Service)
				for _, repo := range repos {
					variable := status.Vars[repo]
					if variable == "" {
						variable = "(unset)"
					}
					fmt.Fprintf(out, "  %s CI_RUNNER=%s\n", repo, variable)
				}
				return nil
			}
			if err := ci.SetRunner(mode, repos, execCI); err != nil {
				return err
			}
			fmt.Fprintf(out, "CI runner set to %s for %d private repositories.\n", mode, len(repos))
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	return cmd
}

func newCISecretsCmd() *cobra.Command {
	var (
		manifestFlag string
		sourceFlag   string
		keysFlag     string
	)
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Push repository secrets declared in workspace.yaml",
	}
	cmd.PersistentFlags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")

	sync := &cobra.Command{
		Use:   "sync [repo]",
		Short: "Set every ci_secrets entry for one repo, or every repo that declares any",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := loadCIManifest(manifestFlag)
			if err != nil {
				return err
			}
			source, err := ci.LoadSource(sourceFlag)
			if err != nil {
				return fmt.Errorf("secret source: %w", err)
			}
			keysDir, err := expandHome(keysFlag)
			if err != nil {
				return err
			}

			wanted := ""
			if len(args) == 1 {
				wanted = args[0]
			}
			var selected []manifest.RepoTarget
			for _, repo := range m.AllRepos() {
				if len(repo.CISecrets) == 0 {
					continue
				}
				if wanted != "" && repo.Name != wanted {
					continue
				}
				selected = append(selected, repo)
			}
			if len(selected) == 0 {
				return fmt.Errorf("no repository with ci_secrets matches %q", wanted)
			}

			out := cmd.OutOrStdout()
			for _, repo := range selected {
				if err := ci.SyncSecrets(repo, source, keysDir, execCIStdin); err != nil {
					return err
				}
				fmt.Fprintf(out, "%s: set %d secret(s)\n", repo.Name, len(repo.CISecrets))
			}
			return nil
		},
	}
	sync.Flags().StringVar(&sourceFlag, "source", defaultCISecretsSource(), "KEY=value file with secret values (chmod 600)")
	sync.Flags().StringVar(&keysFlag, "keys-dir", "~/.config/orbit/deploy-keys", "Directory holding per-repo DEPLOY_SSH_KEY files")

	cmd.AddCommand(sync)
	return cmd
}

func loadCIManifest(manifestFlag string) (*manifest.WorkspaceManifest, error) {
	workspaceRoot := findWorkspaceRoot("")
	manifestPath := findManifestPath(workspaceRoot, manifestFlag)
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load workspace manifest %s: %w", manifestPath, err)
	}
	return m, nil
}

func defaultCISecretsSource() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/orbit/ci-secrets.env"
	}
	return filepath.Join(home, ".config", "orbit", "ci-secrets.env")
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

func execCI(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

func execCIStdin(stdin, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Stdin = strings.NewReader(stdin)
	out, err := command.Output()
	return string(out), err
}
