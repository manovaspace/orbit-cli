// Package repos clones the repositories of the selected projects, points each checkout's git hooks at
// the workspace hooks and fast-forwards clean default branches. It never deletes a repository.
package repos

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	partName     = "repos"
	hooksDirName = ".githooks"
	gitPrompt    = "GIT_TERMINAL_PROMPT=0"
	notSelected  = "not selected; remove with `o remove` or delete by hand"
	notCheckout  = "%s/ exists but is not a git checkout: move it aside and rerun o setup"
	noAccessAll  = "no access to any selected repository"
)

// denialMarkers are the git and ssh messages for a repository this machine may not read.
var denialMarkers = []string{
	"repository not found",
	"403",
	"permission denied",
	"could not read username",
	"authentication failed",
}

type part struct{}

// New returns the repos part. It needs handbook, which holds the workspace manifest.
func New() engine.Part { return part{} }

func (part) Name() string    { return partName }
func (part) Needs() []string { return []string{"handbook"} }

// tally counts selected repositories that cannot be cloned for lack of access, and those that are
// accessible (already cloned, or cloneable).
type tally struct {
	denied     int
	accessible int
}

// Plan works through the selected repositories in order. A missing repository is cloned when the
// remote is readable; a present one gets its hooks path set and, when clean on its default branch
// and behind upstream, a fast-forward. Repositories that are cloned but not selected are reported.
func (part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	selected, err := s.Selected()
	if err != nil {
		return engine.Plan{}, err
	}
	hooksDir := filepath.Join(s.Root, hooksDirName)
	var plan engine.Plan
	var t tally
	chosen := make(map[string]bool, len(selected))
	for _, r := range selected {
		chosen[r.Path] = true
		if r.Path == spec.HandbookPath {
			// The handbook part owns the handbook clone; only its hooks are set here.
			continue
		}
		if err := planSelected(ctx, s, r, hooksDir, &plan, &t); err != nil {
			return engine.Plan{}, err
		}
	}

	if err := planHandbookHooks(ctx, s, hooksDir, &plan); err != nil {
		return engine.Plan{}, err
	}

	if t.denied > 0 && t.accessible == 0 {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  partName,
			Message:  noAccessAll,
			Severity: engine.SeverityError,
		})
	}

	if err := planNotSelected(s, chosen, &plan); err != nil {
		return engine.Plan{}, err
	}
	return plan, nil
}

// planSelected plans one selected repository other than the handbook.
func planSelected(ctx context.Context, s *engine.State, r manifest.RepoTarget, hooksDir string, plan *engine.Plan, t *tally) error {
	dir := filepath.Join(s.Root, r.Path)
	present, err := isCheckout(s, dir)
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot read "+dir))
	}
	if present {
		t.accessible++
		if err := planHooks(ctx, s, r.Path, dir, hooksDir, plan); err != nil {
			return err
		}
		return planFastForward(ctx, s, r, dir, plan)
	}
	return planClone(ctx, s, r, dir, plan, t)
}

// planClone plans the clone of a missing repository once the remote is known to be readable.
func planClone(ctx context.Context, s *engine.State, r manifest.RepoTarget, dir string, plan *engine.Plan, t *tally) error {
	remove, foreign, err := cloneTarget(s, dir)
	if err != nil {
		return err
	}
	if foreign {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  r.Path,
			Message:  fmt.Sprintf(notCheckout, r.Path),
			Severity: engine.SeverityError,
		})
		return nil
	}

	url, err := spec.CloneURL(r.RemoteURL, s.Machine.GitProtocol)
	if err != nil {
		return err
	}
	// ls-remote checks access without cloning. GIT_TERMINAL_PROMPT=0 stops git from prompting.
	res, err := s.Env.RunEnv(ctx, "", []string{gitPrompt}, "git", "ls-remote", "--heads", url)
	switch {
	case err == nil && res.ExitCode == 0:
		t.accessible++
		plan.Actions = append(plan.Actions, cloneAction(s, r, dir, url, remove))
	case res.ExitCode != 0 && isDenied(res):
		t.denied++
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  r.Path,
			Message:  fmt.Sprintf("no access to %s (ask an admin)", r.Name),
			Severity: engine.SeverityWarn,
		})
	case err != nil:
		return clierr.Wrap(err, clierr.Network("cannot check access to "+r.Path))
	default:
		return clierr.Network(fmt.Sprintf("cannot check access to %s: git ls-remote exited %d", r.Path, res.ExitCode))
	}
	return nil
}

// cloneTarget reports whether dir can be cloned into. remove is set when dir exists as an empty
// dir or an interrupted clone, which is removed before cloning. foreign is set for any other
// existing path: the part never deletes user files.
func cloneTarget(s *engine.State, dir string) (remove, foreign bool, err error) {
	_, err = s.FS.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, false, nil
	case errors.Is(err, syscall.ENOTDIR):
		return false, true, nil
	case err != nil:
		return false, false, clierr.Wrap(err, clierr.Failed("cannot read "+dir))
	}
	entries, err := s.FS.ReadDir(dir)
	if err != nil || !onlyInterruptedClone(entries) {
		return false, true, nil
	}
	return true, false, nil
}

// onlyInterruptedClone reports whether entries are empty or hold only a .git directory.
func onlyInterruptedClone(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.Name() != ".git" || !e.IsDir() {
			return false
		}
	}
	return true
}

// cloneAction clones with --filter=blob:none, then sets the hooks path, so one run converges.
// Do removes a partial dir first and creates the parent directory.
func cloneAction(s *engine.State, r manifest.RepoTarget, dir, url string, remove bool) engine.Action {
	hooksDir := filepath.Join(s.Root, hooksDirName)
	return engine.Action{
		Part:    partName,
		Subject: r.Path,
		Summary: "Clone " + r.Path,
		Do: func(ctx context.Context) error {
			if remove {
				if err := s.FS.RemoveAll(dir); err != nil {
					return clierr.Wrap(err, clierr.Failed("cannot remove partial "+dir))
				}
			}
			if err := s.FS.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot create "+filepath.Dir(dir)))
			}
			if _, err := s.Env.RunEnv(ctx, "", []string{gitPrompt}, "git", "clone", "--filter=blob:none", url, dir); err != nil {
				return err
			}
			return setHooksPath(ctx, s, dir, hooksDir)
		},
	}
}

// planHandbookHooks sets the hooks path of the handbook checkout when it is present. A missing
// handbook is cloned by the handbook part, so it is left alone here.
func planHandbookHooks(ctx context.Context, s *engine.State, hooksDir string, plan *engine.Plan) error {
	dir := filepath.Join(s.Root, spec.HandbookPath)
	present, err := isCheckout(s, dir)
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot read "+dir))
	}
	if !present {
		return nil
	}
	return planHooks(ctx, s, spec.HandbookPath, dir, hooksDir, plan)
}

// planHooks plans core.hooksPath=<hooksDir> for a present checkout whose setting differs or is unset.
func planHooks(ctx context.Context, s *engine.State, subject, dir, hooksDir string, plan *engine.Plan) error {
	res, err := s.Run.Run(ctx, "", "git", "-C", dir, "config", "--local", "core.hooksPath")
	var current string
	switch {
	case res.ExitCode == 1:
		// git config exits 1 when the key is unset.
		current = ""
	case err == nil && res.ExitCode == 0:
		current = strings.TrimSpace(res.Stdout)
	case err != nil:
		return clierr.Wrap(err, clierr.Failed("cannot read hooks path of "+subject))
	default:
		return clierr.Failed(fmt.Sprintf("cannot read hooks path of %s: git config exited %d", subject, res.ExitCode))
	}
	if current == hooksDir {
		return nil
	}
	plan.Actions = append(plan.Actions, engine.Action{
		Part:    partName,
		Subject: subject,
		Summary: "Set hooks path for " + subject,
		Do: func(ctx context.Context) error {
			return setHooksPath(ctx, s, dir, hooksDir)
		},
	})
	return nil
}

// setHooksPath runs git -C dir config --local core.hooksPath hooksDir.
func setHooksPath(ctx context.Context, s *engine.State, dir, hooksDir string) error {
	if _, err := s.Run.Run(ctx, "", "git", "-C", dir, "config", "--local", "core.hooksPath", hooksDir); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot set hooks path for "+dir))
	}
	return nil
}

// planFastForward plans git merge --ff-only @{u} when the checkout is on its default branch, has a
// clean tree and is behind its upstream. Any other state is left alone without a finding, except a
// failed fetch, which is a warning.
func planFastForward(ctx context.Context, s *engine.State, r manifest.RepoTarget, dir string, plan *engine.Plan) error {
	branch, ok, err := gitOut(ctx, s, dir, "symbolic-ref", "--short", "HEAD")
	if err != nil || !ok || branch != r.DefaultBranch {
		return err
	}
	status, ok, err := gitOut(ctx, s, dir, "status", "--porcelain")
	if err != nil || !ok || status != "" {
		return err
	}

	res, err := s.Env.RunEnv(ctx, "", []string{gitPrompt}, "git", "-C", dir, "fetch", "--quiet", "origin", r.DefaultBranch)
	if err != nil || res.ExitCode != 0 {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  r.Path,
			Message:  fmt.Sprintf("could not fetch %s: %s", r.Path, fetchReason(res, err)),
			Severity: engine.SeverityWarn,
		})
		return nil
	}

	// No upstream makes rev-list fail, which leaves the checkout untouched.
	behind, ok, err := gitOut(ctx, s, dir, "rev-list", "--count", "HEAD..@{u}")
	if err != nil || !ok {
		return err
	}
	n, err := strconv.Atoi(behind)
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot read commits behind upstream of "+r.Path))
	}
	if n == 0 {
		return nil
	}
	plan.Actions = append(plan.Actions, engine.Action{
		Part:    partName,
		Subject: r.Path,
		Summary: "Fast-forward " + r.Path,
		Do: func(ctx context.Context) error {
			if _, err := s.Run.Run(ctx, "", "git", "-C", dir, "merge", "--ff-only", "@{u}"); err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot fast-forward "+dir))
			}
			return nil
		},
	})
	return nil
}

// planNotSelected warns about every manifest repository that is cloned but not selected. Such a
// repository is never deleted here.
func planNotSelected(s *engine.State, chosen map[string]bool, plan *engine.Plan) error {
	seen := make(map[string]bool)
	for _, r := range s.Manifest.AllRepos() {
		if seen[r.Path] || chosen[r.Path] || r.Path == spec.HandbookPath {
			continue
		}
		seen[r.Path] = true
		present, err := isCheckout(s, filepath.Join(s.Root, r.Path))
		if err != nil {
			return clierr.Wrap(err, clierr.Failed("cannot read "+r.Path))
		}
		if present {
			plan.Findings = append(plan.Findings, engine.Finding{
				Part:     partName,
				Subject:  r.Path,
				Message:  notSelected,
				Severity: engine.SeverityWarn,
			})
		}
	}
	return nil
}

// isCheckout reports whether dir holds a git checkout, judged by .git/HEAD. A missing path, or one
// whose parent is a regular file, is not a checkout.
func isCheckout(s *engine.State, dir string) (bool, error) {
	_, err := s.FS.Stat(filepath.Join(dir, ".git", "HEAD"))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return false, nil
	default:
		return false, err
	}
}

// gitOut runs git -C dir args. It returns the trimmed stdout and ok=true when git exits 0. A non-zero
// exit is ok=false with a nil error; a command that could not run at all is an error.
func gitOut(ctx context.Context, s *engine.State, dir string, args ...string) (string, bool, error) {
	res, err := s.Run.Run(ctx, "", "git", append([]string{"-C", dir}, args...)...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", false, ctxErr
	}
	if err == nil && res.ExitCode == 0 {
		return strings.TrimSpace(res.Stdout), true, nil
	}
	if res.ExitCode != 0 {
		return "", false, nil
	}
	return "", false, clierr.Wrap(err, clierr.Tool("cannot run git in "+dir))
}

// isDenied reports whether git or ssh output says the machine may not read the repository.
func isDenied(res runner.Result) bool {
	text := strings.ToLower(res.Stdout + "\n" + res.Stderr)
	for _, m := range denialMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// fetchReason is the first stderr line of a failed fetch, or the run error when git printed nothing.
func fetchReason(res runner.Result, err error) string {
	line, _, _ := strings.Cut(strings.TrimSpace(res.Stderr), "\n")
	if line != "" {
		return line
	}
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("git exited %d", res.ExitCode)
}
