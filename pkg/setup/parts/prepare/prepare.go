// Package prepare runs the commands each selected repository declares in orbit.yaml. Prepare
// commands run when a lockfile (bun.lock, bun.lockb, package-lock.json, go.sum; depth 3 at most,
// outside node_modules and dot-directories) changed. Migrate commands run when the repository's
// git HEAD moved since the last migrate or when one of its databases was just created. Seed
// commands run only when one of its databases was just created. Every command runs with sh -c in
// the repository, with the KEY=VALUE pairs of its written env files added. Progress is kept in
// $XDG_STATE_HOME/orbit/prepare.json. With services: external, migrate and seed are skipped for
// repositories that declare Postgres databases.
package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/env/domain"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	partName       = "prepare"
	stateName      = "prepare.json"
	defaultSchema  = ".env.schema.yaml"
	defaultWrite   = ".env"
	maxLockDepth   = 3
	stderrTailSize = 20
	externalMode   = "external"
)

// lockfiles are the names whose contents feed the lock hash.
var lockfiles = map[string]bool{"bun.lock": true, "bun.lockb": true, "package-lock.json": true, "go.sum": true}

// part runs the repository commands. statePath locates prepare.json; production uses
// $XDG_STATE_HOME/orbit/prepare.json, tests inject their own.
type part struct {
	statePath func() (string, error)
}

// entry is what prepare remembers about one repository.
type entry struct {
	LockHash     string `json:"lock_hash"`
	MigratedHead string `json:"migrated_head"`
	PendingSeed  bool   `json:"pending_seed,omitempty"`
}

// stateFile maps a workspace repository path to its entry.
type stateFile map[string]entry

// New returns the prepare part. It needs env, so the env files a command reads exist first.
func New() engine.Part { return part{statePath: productionStatePath} }

func (part) Name() string    { return partName }
func (part) Needs() []string { return []string{"env"} }

// productionStatePath is $XDG_STATE_HOME/orbit/prepare.json, else ~/.local/state/orbit/prepare.json.
func productionStatePath() (string, error) {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "orbit", stateName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.Wrap(err, clierr.Failed("cannot find the home directory"))
	}
	return filepath.Join(home, ".local", "state", "orbit", stateName), nil
}

// Plan reads the state file and plans the commands each selected repository needs. Nothing runs.
func (p part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	selected, err := s.Selected()
	if err != nil {
		return engine.Plan{}, err
	}
	path, err := p.statePath()
	if err != nil {
		return engine.Plan{}, err
	}
	st, corrupt, err := loadState(s.FS, path)
	if err != nil {
		return engine.Plan{}, err
	}
	var plan engine.Plan
	if corrupt {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  partName,
			Message:  "prepare state unreadable; rerunning prepare",
			Fix:      "nothing to do; the state file is rewritten by the next successful run",
			Severity: engine.SeverityWarn,
		})
	}
	for _, target := range selected {
		if err := planRepo(ctx, s, target, path, st, &plan); err != nil {
			return engine.Plan{}, err
		}
	}
	return plan, nil
}

// planRepo plans the prepare, migrate and seed commands of one repository, in that order. A
// repository without orbit.yaml contributes nothing.
func planRepo(ctx context.Context, s *engine.State, target manifest.RepoTarget, statePath string, st stateFile, plan *engine.Plan) error {
	repo := target.Path
	dir := filepath.Join(s.Root, repo)
	rs, found, err := spec.LoadRepoSpec(dir)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	writes, err := envWrites(s.FS, dir, rs)
	if err != nil {
		return err
	}
	cur := st[repo]

	var repoActions []engine.Action
	if len(rs.Prepare) > 0 {
		lock, err := lockHash(s.FS, dir)
		if err != nil {
			return err
		}
		if cur.LockHash != lock {
			done := func(context.Context) error {
				return record(s.FS, statePath, repo, func(e *entry) { e.LockHash = lock })
			}
			repoActions = append(repoActions, commandActions(s, repo, dir, writes, rs.Prepare, done)...)
		}
	}

	shared := s.Machine.Services == externalMode && rs.Services.Postgres != nil
	if shared {
		plan.Actions = append(plan.Actions, repoActions...)
		if len(rs.Migrate) > 0 || len(rs.Seed) > 0 {
			plan.Findings = append(plan.Findings, engine.Finding{
				Part:     partName,
				Subject:  repo,
				Message:  fmt.Sprintf("%s: migrations not run against shared databases (services: %s)", repo, externalMode),
				Fix:      "run migrations against your own database by hand, or set services: native in machine.yaml",
				Severity: engine.SeverityWarn,
			})
		}
		return nil
	}

	newDB := hasNewDatabase(s, rs)
	if len(rs.Migrate) > 0 {
		head, err := gitHead(ctx, s, dir)
		if err != nil {
			return err
		}
		if cur.MigratedHead != head || newDB {
			done := func(context.Context) error {
				return record(s.FS, statePath, repo, func(e *entry) { e.MigratedHead = head })
			}
			repoActions = append(repoActions, commandActions(s, repo, dir, writes, rs.Migrate, done)...)
		}
	}
	if (newDB || cur.PendingSeed) && len(rs.Seed) > 0 {
		done := func(context.Context) error {
			return record(s.FS, statePath, repo, func(e *entry) { e.PendingSeed = false })
		}
		repoActions = append(repoActions, commandActions(s, repo, dir, writes, rs.Seed, done)...)
	}
	if newDB && len(rs.Seed) > 0 && !cur.PendingSeed && len(repoActions) > 0 {
		firstDo := repoActions[0].Do
		repoActions[0].Do = func(ctx context.Context) error {
			if err := record(s.FS, statePath, repo, func(e *entry) { e.PendingSeed = true }); err != nil {
				return err
			}
			return firstDo(ctx)
		}
	}
	plan.Actions = append(plan.Actions, repoActions...)
	return nil
}

// commandActions makes one action per command. The last action runs done after its command
// succeeds, so the state entry changes only once every command of the kind has passed.
func commandActions(s *engine.State, repo, dir string, writes, commands []string, done func(context.Context) error) []engine.Action {
	out := make([]engine.Action, 0, len(commands))
	for i, command := range commands {
		last := i == len(commands)-1
		out = append(out, engine.Action{
			Part:    partName,
			Subject: repo,
			Summary: repo + ": " + command,
			Do: func(ctx context.Context) error {
				if err := runCommand(ctx, s, repo, dir, writes, command); err != nil {
					return err
				}
				if last && done != nil {
					return done(ctx)
				}
				return nil
			},
		})
	}
	return out
}

// runCommand runs command with sh -c in dir, with the env of the written files added. On failure
// the error names the command and carries the last lines of its stderr, or of its stdout when
// stderr is empty.
func runCommand(ctx context.Context, s *engine.State, repo, dir string, writes []string, command string) error {
	env, err := readEnv(s.FS, dir, writes)
	if err != nil {
		return err
	}
	if os.Getenv("GOTOOLCHAIN") == "" && !hasEnvKey(env, "GOTOOLCHAIN") {
		env = append([]string{"GOTOOLCHAIN=auto"}, env...)
	}
	res, err := s.Env.RunEnv(ctx, dir, env, "sh", "-c", command)
	if err != nil {
		msg := fmt.Sprintf("%s: %s failed", repo, command)
		out := res.Stderr
		if strings.TrimSpace(out) == "" {
			out = res.Stdout
		}
		if tail := tailLines(out, stderrTailSize); tail != "" {
			msg += "\n" + tail
		}
		return clierr.WrapClean(err, clierr.Tool(msg))
	}
	return nil
}

// hasEnvKey reports whether env already defines key=value.
func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

// tailLines returns the last n lines of text, trailing newlines removed.
func tailLines(text string, n int) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// envWrites lists the env files a repository writes, relative to it. Without an env list, a root
// schema means the default .env, as the env part writes it.
func envWrites(fsys engine.FS, dir string, rs spec.RepoSpec) ([]string, error) {
	if len(rs.Env) > 0 {
		out := make([]string, 0, len(rs.Env))
		for _, e := range rs.Env {
			out = append(out, e.Write)
		}
		return out, nil
	}
	_, err := fsys.Stat(filepath.Join(dir, defaultSchema))
	switch {
	case err == nil:
		return []string{defaultWrite}, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	default:
		return nil, clierr.Wrap(err, clierr.Failed("cannot read "+filepath.Join(dir, defaultSchema)))
	}
}

// readEnv returns KEY=VALUE pairs from the written files, in file order: the first file that
// defines a key wins. A missing file adds nothing. Keys are sorted for a stable command environment.
func readEnv(fsys engine.FS, dir string, writes []string) ([]string, error) {
	values := map[string]string{}
	for _, rel := range writes {
		path := filepath.Join(dir, rel)
		data, err := fsys.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, clierr.Wrap(err, clierr.Failed("cannot read "+path))
		}
		parsed, err := domain.ParseEnvContent(string(data))
		if err != nil {
			return nil, clierr.Wrap(err, clierr.Failed(path+" is malformed"))
		}
		for k, v := range parsed {
			if _, ok := values[k]; !ok {
				values[k] = v
			}
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+values[k])
	}
	return out, nil
}

// hasNewDatabase reports whether any database the repository declares was just created.
func hasNewDatabase(s *engine.State, rs spec.RepoSpec) bool {
	if rs.Services.Postgres == nil {
		return false
	}
	for _, db := range rs.Services.Postgres.Databases {
		if s.NewDatabases[db.Name] {
			return true
		}
	}
	return false
}

// gitHead returns the commit the repository has checked out.
func gitHead(ctx context.Context, s *engine.State, dir string) (string, error) {
	res, err := s.Run.Run(ctx, dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", clierr.Wrap(err, clierr.Failed("cannot read git HEAD of "+dir))
	}
	return strings.TrimSpace(res.Stdout), nil
}

// lockHash is the SHA-256 over the sorted (relative path, file SHA-256) pairs of the lockfiles at
// depth 3 or less below dir, skipping node_modules and dot-directories.
func lockHash(fsys engine.FS, dir string) (string, error) {
	var found []string
	if err := collectLocks(fsys, dir, "", 0, &found); err != nil {
		return "", err
	}
	sort.Strings(found)
	h := sha256.New()
	for _, rel := range found {
		data, err := fsys.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", clierr.Wrap(err, clierr.Failed("cannot read "+filepath.Join(dir, rel)))
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s %x\n", filepath.ToSlash(rel), sum)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// collectLocks appends the relative paths of lockfiles under dir/rel. segs is the number of path
// segments of rel; a file in rel has segs+1 segments and may have at most maxLockDepth.
func collectLocks(fsys engine.FS, dir, rel string, segs int, found *[]string) error {
	entries, err := fsys.ReadDir(filepath.Join(dir, rel))
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot read "+filepath.Join(dir, rel)))
	}
	for _, e := range entries {
		name := e.Name()
		childRel := filepath.Join(rel, name)
		if e.IsDir() {
			if name == "node_modules" || strings.HasPrefix(name, ".") || segs+1 >= maxLockDepth {
				continue
			}
			if err := collectLocks(fsys, dir, childRel, segs+1, found); err != nil {
				return err
			}
			continue
		}
		if lockfiles[name] && segs+1 <= maxLockDepth {
			*found = append(*found, childRel)
		}
	}
	return nil
}

// loadState reads the state file. A missing file is an empty state. A file that does not parse is
// an empty state with corrupt set, so the caller can warn; it is replaced by the next save.
func loadState(fsys engine.FS, path string) (stateFile, bool, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return stateFile{}, false, nil
	}
	if err != nil {
		return nil, false, clierr.Wrap(err, clierr.Failed("cannot read "+path))
	}
	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return stateFile{}, true, nil
	}
	if st == nil {
		st = stateFile{}
	}
	return st, false, nil
}

// record loads the state file afresh, applies set to the entry of repo and saves it.
func record(fsys engine.FS, path, repo string, set func(*entry)) error {
	st, _, err := loadState(fsys, path)
	if err != nil {
		return err
	}
	e := st[repo]
	set(&e)
	st[repo] = e
	return saveState(fsys, path, st)
}

// saveState writes st to path: directory 0700, file 0600, replaced atomically.
func saveState(fsys engine.FS, path string, st stateFile) error {
	dir := filepath.Dir(path)
	if err := fsys.MkdirAll(dir, 0o700); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot create "+dir))
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot encode the prepare state"))
	}
	return writeAtomic(fsys, path, append(data, '\n'))
}

// writeAtomic writes data to a temporary file beside path with mode 0600 and renames it over path.
func writeAtomic(fsys engine.FS, path string, data []byte) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".orbit-tmp")
	_ = fsys.Remove(tmp)
	if err := fsys.WriteFile(tmp, data, 0o600); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot write "+path))
	}
	if err := fsys.Rename(tmp, path); err != nil {
		_ = fsys.Remove(tmp)
		return clierr.Wrap(err, clierr.Failed("cannot replace "+path))
	}
	return nil
}
