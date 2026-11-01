package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ciapp "github.com/manovaspace/orbit-cli/pkg/ci/application"
	cidomain "github.com/manovaspace/orbit-cli/pkg/ci/domain"
	cifiles "github.com/manovaspace/orbit-cli/pkg/ci/infrastructure/files"
	"github.com/manovaspace/orbit-cli/pkg/ci/infrastructure/ghcli"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/spf13/cobra"
)

// The CI commands reach gh, systemctl and secret files through these seams so
// tests can swap the adapters.
var (
	ciCommands ciapp.Command = ghcli.New(runner.Exec{})
	ciFiles    ciapp.Files   = cifiles.New()
)

func newCIService(out io.Writer) *ciapp.Service {
	return &ciapp.Service{Commands: ciCommands, Files: ciFiles, Out: out}
}

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
	var (
		manifestFlag string
		jsonFlag     bool
	)
	cmd := &cobra.Command{
		Use:   "runner vps|github|status",
		Short: "Start or stop the standby runner and set CI_RUNNER on private repos",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			mode, err := cidomain.ParseRunnerMode(args[0])
			if err != nil {
				usageErr := clierr.Usage(err.Error())
				writeCIFailure(out, jsonFlag, usageErr)
				return usageErr
			}
			m, err := loadCIManifest(manifestFlag)
			if err != nil {
				writeCIFailure(out, jsonFlag, err)
				return err
			}
			svc := newCIService(uiOut)
			repos, err := svc.PrivateRepos(cmd.Context(), m.AllRepos())
			if err != nil {
				writeCIFailure(out, jsonFlag, err)
				return err
			}

			if mode == cidomain.ModeStatus {
				status, err := svc.RunnerStatus(cmd.Context(), repos)
				if err != nil {
					writeCIFailure(out, jsonFlag, err)
					return err
				}
				if jsonFlag {
					return output.Write(out, output.Success(status))
				}
				svc.RenderStatus(status, repos)
				return nil
			}

			if err := svc.SetRunner(cmd.Context(), mode, repos); err != nil {
				writeCIFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(ciapp.SetResult{Mode: string(mode), Repositories: repos}))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newCISecretsCmd() *cobra.Command {
	var manifestFlag string
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Push repository secrets declared in workspace.yaml",
	}
	cmd.PersistentFlags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")

	var (
		sourceFlag string
		keysFlag   string
		jsonFlag   bool
	)
	sync := &cobra.Command{
		Use:   "sync [repo]",
		Short: "Set every ci_secrets entry for one repo, or every repo that declares any",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			m, err := loadCIManifest(manifestFlag)
			if err != nil {
				writeCIFailure(out, jsonFlag, err)
				return err
			}
			svc := newCIService(uiOut)
			source, err := svc.LoadSource(sourceFlag)
			if err != nil {
				sourceErr := clierr.Wrap(err, clierr.Failed("secret source"))
				writeCIFailure(out, jsonFlag, sourceErr)
				return sourceErr
			}
			keysDir, err := expandHome(keysFlag)
			if err != nil {
				writeCIFailure(out, jsonFlag, err)
				return err
			}

			wanted := ""
			if len(args) == 1 {
				wanted = args[0]
			}
			res, err := svc.Sync(cmd.Context(), m.AllRepos(), wanted, source, keysDir)
			if err != nil {
				writeCIFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}
	sync.Flags().StringVar(&sourceFlag, "source", defaultCISecretsSource(), "KEY=value file with secret values (chmod 600)")
	sync.Flags().StringVar(&keysFlag, "keys-dir", "~/.config/orbit/deploy-keys", "Directory holding per-repo DEPLOY_SSH_KEY files")
	sync.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	cmd.AddCommand(sync)
	return cmd
}

func writeCIFailure(out io.Writer, jsonFlag bool, err error) {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
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
