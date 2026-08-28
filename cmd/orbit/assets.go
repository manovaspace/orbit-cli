package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/assets"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/spf13/cobra"
)

func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assets",
		Short: "Sync gitignored media with private Cloudflare R2",
		Long:  "Pull, push, add, and status for files listed in orbit-assets.yaml. Git stores the index; R2 stores bytes.",
	}
	cmd.AddCommand(newAssetsPullCmd())
	cmd.AddCommand(newAssetsPushCmd())
	cmd.AddCommand(newAssetsAddCmd())
	cmd.AddCommand(newAssetsStatusCmd())
	return cmd
}

func newAssetsPullCmd() *cobra.Command {
	var force bool
	var manifestFlag string
	cmd := &cobra.Command{
		Use:   "pull [scope]",
		Short: "Download missing or outdated assets for workspace repos",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}
			root, rels, err := resolveAssetRepos(manifestFlag, scope)
			if err != nil {
				return err
			}
			store, err := assets.OpenStore(cmd.Context())
			if err != nil {
				return fmt.Errorf("R2 credentials (%s): %w", assets.DefaultR2EnvPath(), err)
			}
			if err := assets.PullRepos(cmd.Context(), root, rels, store, assets.PullOptions{Force: force}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Assets pulled.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite local files whose hash does not match the index")
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	return cmd
}

func newAssetsPushCmd() *cobra.Command {
	var manifestFlag string
	cmd := &cobra.Command{
		Use:   "push [scope]",
		Short: "Upload index objects that are missing from R2",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}
			root, rels, err := resolveAssetRepos(manifestFlag, scope)
			if err != nil {
				return err
			}
			store, err := assets.OpenStore(cmd.Context())
			if err != nil {
				return fmt.Errorf("R2 credentials (%s): %w", assets.DefaultR2EnvPath(), err)
			}
			if err := assets.PushRepos(cmd.Context(), root, rels, store); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Assets pushed.")
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	return cmd
}

func newAssetsAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Hash, upload, and gitignore a file; update orbit-assets.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			repoRoot, rel, err := findRepoRelative(abs)
			if err != nil {
				return err
			}
			store, err := assets.OpenStore(cmd.Context())
			if err != nil {
				return fmt.Errorf("R2 credentials (%s): %w", assets.DefaultR2EnvPath(), err)
			}
			obj, err := assets.Add(cmd.Context(), repoRoot, rel, store)
			if err != nil {
				return err
			}
			if assets.WarnSmall(obj.Size) {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s is %d bytes; consider committing small icons to git instead\n", rel, obj.Size)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s (%s, %d bytes). Commit orbit-assets.yaml and .gitignore.\n", rel, obj.SHA256[:12], obj.Size)
			return nil
		},
	}
	return cmd
}

func newAssetsStatusCmd() *cobra.Command {
	var manifestFlag string
	cmd := &cobra.Command{
		Use:   "status [scope]",
		Short: "Show missing or mismatched local assets",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "all"
			if len(args) > 0 && args[0] != "" {
				scope = args[0]
			}
			root, rels, err := resolveAssetRepos(manifestFlag, scope)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			problems := 0
			for _, rel := range rels {
				repo := filepath.Join(root, rel)
				if !assets.HasManifest(repo) {
					continue
				}
				st, err := assets.Status(repo)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", rel)
				for _, s := range st {
					mark := "ok"
					if s.State != assets.FileOK {
						problems++
						mark = string(s.State)
					}
					fmt.Fprintf(out, "  %s  %s\n", mark, s.Path)
				}
			}
			if problems > 0 {
				return fmt.Errorf("%d asset(s) missing or mismatched", problems)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	return cmd
}

func resolveAssetRepos(manifestFlag, scope string) (workspaceRoot string, rels []string, err error) {
	workspaceRoot = findWorkspaceRoot("")
	mp := findManifestPath(workspaceRoot, manifestFlag)
	m, err := manifest.Load(mp)
	if err != nil {
		return "", nil, err
	}
	targets := m.ResolveScope(scope)
	rels = make([]string, 0, len(targets))
	for _, t := range targets {
		rels = append(rels, t.Path)
	}
	return workspaceRoot, rels, nil
}

func addAssetDiagnostics(ctx context.Context, report *doctor.DoctorReport, fix bool) {
	if report == nil {
		return
	}
	workspaceRoot := findWorkspaceRoot("")
	mp := findManifestPath(workspaceRoot, "")
	m, err := manifest.Load(mp)
	if err != nil {
		return
	}
	targets := m.ResolveScope("all")
	rels := make([]string, 0, len(targets))
	any := false
	for _, t := range targets {
		rels = append(rels, t.Path)
		if assets.HasManifest(filepath.Join(workspaceRoot, t.Path)) {
			any = true
		}
	}
	if !any {
		return
	}

	if _, err := os.Stat(assets.DefaultR2EnvPath()); err != nil {
		report.Add(doctor.DiagnosticResult{
			Category:      "Assets",
			Name:          "R2 credentials",
			Status:        doctor.StatusError,
			Message:       fmt.Sprintf("missing %s", assets.DefaultR2EnvPath()),
			FixSuggestion: "Create ~/.config/orbit/r2.env with R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET=manova-assets",
		})
		return
	}

	store, err := assets.OpenStore(ctx)
	if err != nil {
		report.Add(doctor.DiagnosticResult{
			Category:      "Assets",
			Name:          "R2 credentials",
			Status:        doctor.StatusError,
			Message:       err.Error(),
			FixSuggestion: "Fix ~/.config/orbit/r2.env and confirm the manova-assets bucket exists",
		})
		return
	}
	if pinger, ok := store.(interface{ Ping(context.Context) error }); ok {
		if err := pinger.Ping(ctx); err != nil {
			report.Add(doctor.DiagnosticResult{
				Category:      "Assets",
				Name:          "R2 bucket",
				Status:        doctor.StatusError,
				Message:       err.Error(),
				FixSuggestion: "Create private R2 bucket manova-assets and grant the API token Object Read & Write",
			})
			return
		}
	}

	if fix {
		if err := assets.PullRepos(ctx, workspaceRoot, rels, store, assets.PullOptions{}); err != nil {
			report.Add(doctor.DiagnosticResult{
				Category: "Assets",
				Name:     "orbit assets pull",
				Status:   doctor.StatusError,
				Message:  err.Error(),
			})
		}
	}

	missing := 0
	for _, rel := range rels {
		repo := filepath.Join(workspaceRoot, rel)
		if !assets.HasManifest(repo) {
			continue
		}
		st, err := assets.Status(repo)
		if err != nil {
			report.Add(doctor.DiagnosticResult{
				Category: "Assets",
				Name:     rel,
				Status:   doctor.StatusError,
				Message:  err.Error(),
			})
			continue
		}
		for _, s := range st {
			if s.State != assets.FileOK {
				missing++
			}
		}
	}
	if missing > 0 {
		report.Add(doctor.DiagnosticResult{
			Category:      "Assets",
			Name:          "Local media",
			Status:        doctor.StatusError,
			Message:       fmt.Sprintf("%d file(s) missing or hash-mismatched vs orbit-assets.yaml", missing),
			FixSuggestion: "Run orbit doctor --fix or orbit assets pull",
		})
		return
	}
	report.Add(doctor.DiagnosticResult{
		Category: "Assets",
		Name:     "Local media",
		Status:   doctor.StatusOK,
		Message:  "orbit-assets.yaml files are present and match",
	})
}

func findRepoRelative(absFile string) (repoRoot, rel string, err error) {
	dir := filepath.Dir(absFile)
	for {
		if _, e := os.Stat(filepath.Join(dir, ".git")); e == nil {
			rel, err = filepath.Rel(dir, absFile)
			if err != nil {
				return "", "", err
			}
			return dir, filepath.ToSlash(rel), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("%s is not inside a git repository", absFile)
		}
		dir = parent
	}
}

func pullWorkspaceAssets(ctx context.Context, workspaceRoot string, rels []string, force bool) error {
	any := false
	for _, rel := range rels {
		if assets.HasManifest(filepath.Join(workspaceRoot, rel)) {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	store, err := assets.OpenStore(ctx)
	if err != nil {
		return fmt.Errorf("R2 credentials (%s): %w", assets.DefaultR2EnvPath(), err)
	}
	return assets.PullRepos(ctx, workspaceRoot, rels, store, assets.PullOptions{Force: force})
}
