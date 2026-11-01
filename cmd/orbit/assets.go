package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	assetsapp "github.com/manovaspace/orbit-cli/pkg/assets/application"
	assetsdomain "github.com/manovaspace/orbit-cli/pkg/assets/domain"
	assetsosfs "github.com/manovaspace/orbit-cli/pkg/assets/infrastructure/osfs"
	assetsr2 "github.com/manovaspace/orbit-cli/pkg/assets/infrastructure/r2"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/spf13/cobra"
)

// The asset commands reach the filesystem and R2 through these seams so tests
// can swap the adapters.
var (
	assetsFS        assetsapp.FS = assetsosfs.New()
	openAssetsStore              = func(ctx context.Context) (assetsapp.Store, error) { return assetsr2.OpenStore(ctx) }
)

func newAssetsService(out io.Writer, store assetsapp.Store) *assetsapp.Service {
	return &assetsapp.Service{FS: assetsFS, R2: store, Out: out}
}

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
	var (
		force        bool
		manifestFlag string
		jsonFlag     bool
	)
	cmd := &cobra.Command{
		Use:   "pull [scope]",
		Short: "Download missing or outdated assets for workspace repos",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			root, rels, err := resolveAssetRepos(manifestFlag, assetScope(args))
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			store, err := openAssets(cmd.Context())
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			res, err := newAssetsService(uiOut, store).Pull(cmd.Context(), root, rels, assetsapp.PullOptions{Force: force})
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite local files whose hash does not match the index")
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newAssetsPushCmd() *cobra.Command {
	var (
		manifestFlag string
		jsonFlag     bool
	)
	cmd := &cobra.Command{
		Use:   "push [scope]",
		Short: "Upload index objects that are missing from R2",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			root, rels, err := resolveAssetRepos(manifestFlag, assetScope(args))
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			store, err := openAssets(cmd.Context())
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			res, err := newAssetsService(uiOut, store).Push(cmd.Context(), root, rels)
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
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
			store, err := openAssets(cmd.Context())
			if err != nil {
				return err
			}
			res, err := newAssetsService(cmd.OutOrStdout(), store).Add(cmd.Context(), repoRoot, rel)
			if err != nil {
				return err
			}
			if res.Small {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s is %d bytes; consider committing small icons to git instead\n", rel, res.Object.Size)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s (%s, %d bytes). Commit orbit-assets.yaml and .gitignore.\n", rel, res.Object.SHA256[:12], res.Object.Size)
			return nil
		},
	}
	return cmd
}

func newAssetsStatusCmd() *cobra.Command {
	var (
		manifestFlag string
		jsonFlag     bool
	)
	cmd := &cobra.Command{
		Use:   "status [scope]",
		Short: "Show missing or mismatched local assets",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			root, rels, err := resolveAssetRepos(manifestFlag, assetScope(args))
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			// Status is local: it reads the index and hashes files without
			// touching R2 or requiring credentials.
			res, err := newAssetsService(uiOut, nil).Status(cmd.Context(), root, rels)
			if err != nil {
				writeAssetsFailure(out, jsonFlag, err)
				return err
			}
			if res.Problems > 0 {
				problemErr := clierr.Failed(fmt.Sprintf("%d asset(s) missing or mismatched", res.Problems))
				if jsonFlag {
					_ = output.Write(out, output.Failure(problemErr))
				}
				return problemErr
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func assetScope(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return "all"
}

func writeAssetsFailure(out io.Writer, jsonFlag bool, err error) {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
}

// openAssets opens the R2 store, naming the credentials file in the error the
// same way the command always has.
func openAssets(ctx context.Context) (assetsapp.Store, error) {
	store, err := openAssetsStore(ctx)
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed(fmt.Sprintf("R2 credentials (%s)", assetsr2.DefaultEnvPath())))
	}
	return store, nil
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

// addSelectedAssetDiagnostics keeps local inspection free of cloud credential activity.
func addSelectedAssetDiagnostics(ctx context.Context, report *doctor.DoctorReport, remote, fix bool) {
	if remote {
		addAssetDiagnostics(ctx, report, fix)
		return
	}
	report.Add(doctor.DiagnosticResult{Category: "Assets", Name: "R2 assets", Status: doctor.StatusUnverified, Message: "Cloud assets unverified (local mode); select --remote to probe"})
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
		if has, err := newAssetsService(io.Discard, nil).HasManifest(filepath.Join(workspaceRoot, t.Path)); err == nil && has {
			any = true
		}
	}
	if !any {
		return
	}

	if _, err := os.Stat(assetsr2.DefaultEnvPath()); err != nil {
		report.Add(doctor.DiagnosticResult{
			Category:      "Assets",
			Name:          "R2 credentials",
			Status:        doctor.StatusError,
			Message:       fmt.Sprintf("missing %s", assetsr2.DefaultEnvPath()),
			FixSuggestion: "Create ~/.config/orbit/r2.env with R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET=manova-assets",
		})
		return
	}

	store, err := openAssetsStore(ctx)
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

	svc := newAssetsService(io.Discard, store)
	if fix {
		if _, err := svc.Pull(ctx, workspaceRoot, rels, assetsapp.PullOptions{}); err != nil {
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
		if has, err := svc.HasManifest(repo); err != nil || !has {
			continue
		}
		st, err := svc.Status(ctx, workspaceRoot, []string{rel})
		if err != nil {
			report.Add(doctor.DiagnosticResult{
				Category: "Assets",
				Name:     rel,
				Status:   doctor.StatusError,
				Message:  err.Error(),
			})
			continue
		}
		for _, repository := range st.Repositories {
			for _, file := range repository.Files {
				if file.State != assetsdomain.FileOK {
					missing++
				}
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
	hasAny := false
	for _, rel := range rels {
		if has, err := newAssetsService(io.Discard, nil).HasManifest(filepath.Join(workspaceRoot, rel)); err == nil && has {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}
	store, err := openAssets(ctx)
	if err != nil {
		return err
	}
	_, err = newAssetsService(io.Discard, store).Pull(ctx, workspaceRoot, rels, assetsapp.PullOptions{Force: force})
	return err
}
