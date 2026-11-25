// Package root rebuilds the workspace root folder from handbook/agents/workspace/root.yaml: symlinks
// into the handbook, small copied files, the generated go.work and generator commands. A hand-made
// file or directory is never overwritten or deleted; it is reported as a conflict.
package root

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	partName = "root"
	// specRel is the root declaration, relative to the workspace root.
	specRel = "handbook/agents/workspace/root.yaml"
	// stateRel is the generator state file, relative to the XDG state home.
	stateRel = "orbit/root.json"
)

// part reconciles the workspace root. state overrides the generator state file path; empty means
// the XDG state location.
type part struct {
	state string
}

// New returns the root part. It needs repos, so every selected repository is in place before the
// go.work file lists its modules.
func New() engine.Part { return part{} }

func (part) Name() string    { return partName }
func (part) Needs() []string { return []string{"repos"} }

// Plan reads the declaration, plans the links, files and go.work, then the generators. A generator
// runs when the plan changed something at the root or when it has never been recorded as run.
func (p part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	rs, err := loadSpec(s)
	if err != nil {
		return engine.Plan{}, err
	}
	plan, err := scan(s.FS, s.Root, rs)
	if err != nil {
		return engine.Plan{}, err
	}

	for _, key := range sortedKeys(rs.Generated) {
		goPlan, err := planGoWork(s, key)
		if err != nil {
			return engine.Plan{}, err
		}
		plan.Actions = append(plan.Actions, goPlan.Actions...)
		plan.Findings = append(plan.Findings, goPlan.Findings...)
	}

	if len(rs.Generators) == 0 {
		return plan, nil
	}
	changed := len(plan.Actions) > 0
	gens, err := p.planGenerators(s, rs.Generators, changed)
	if err != nil {
		return engine.Plan{}, err
	}
	plan.Actions = append(plan.Actions, gens...)
	return plan, nil
}

// Inspect reports how the links and files declared in rs differ from the workspace at root. Conflicts
// and missing sources are reported as they are; every link or file that is not in place is a warning.
// Inspect plans nothing and runs no generator, and it does not check go.work.
func Inspect(fsys engine.FS, root string, rs spec.RootSpec) []engine.Finding {
	plan, err := scan(fsys, root, rs)
	if err != nil {
		return []engine.Finding{{
			Part:     partName,
			Subject:  partName,
			Message:  "cannot inspect the workspace root: " + err.Error(),
			Severity: engine.SeverityError,
		}}
	}
	findings := append([]engine.Finding(nil), plan.Findings...)
	for _, a := range plan.Actions {
		findings = append(findings, engine.Finding{
			Part:     partName,
			Subject:  a.Subject,
			Message:  "not as declared in root.yaml: " + a.Summary,
			Severity: engine.SeverityWarn,
		})
	}
	return findings
}

// loadSpec reads and validates the declaration. A missing declaration means the handbook is not
// cloned, so the root cannot be checked.
func loadSpec(s *engine.State) (spec.RootSpec, error) {
	path := filepath.Join(s.Root, specRel)
	data, err := s.FS.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return spec.RootSpec{}, clierr.Unverified("root spec " + path + " not found: the handbook is not cloned yet")
	case err != nil:
		return spec.RootSpec{}, clierr.Wrap(err, clierr.Failed("cannot read root spec "+path))
	}
	rs, err := spec.ParseRootSpec(data)
	if err != nil {
		return spec.RootSpec{}, clierr.Wrap(err, clierr.Usage("root spec "+path+" is invalid"))
	}
	if rs.Version != 1 {
		return spec.RootSpec{}, clierr.Usage(fmt.Sprintf("root spec %s: version %d is not supported; want 1", path, rs.Version))
	}
	return rs, nil
}

// planner collects the actions and findings of one scan of the root.
type planner struct {
	fsys engine.FS
	root string
	plan engine.Plan
}

// scan plans the links and then the files of rs, in key order. It never writes.
func scan(fsys engine.FS, root string, rs spec.RootSpec) (engine.Plan, error) {
	p := &planner{fsys: fsys, root: root}
	for _, target := range sortedKeys(rs.Links) {
		if err := p.link(target, rs.Links[target]); err != nil {
			return engine.Plan{}, err
		}
	}
	for _, target := range sortedKeys(rs.Files) {
		if err := p.file(target, rs.Files[target]); err != nil {
			return engine.Plan{}, err
		}
	}
	return p.plan, nil
}

// link plans the symlink at target that points at source. A link that already resolves to source,
// or a regular file with the same bytes as source, is in place.
func (p *planner) link(target, src string) error {
	source := filepath.Join(p.root, filepath.FromSlash(src))
	dest := filepath.Join(p.root, filepath.FromSlash(target))
	srcInfo, err := p.fsys.Stat(source)
	if isMissing(err) {
		p.finding(target, engine.SeverityWarn, fmt.Sprintf("source %s is missing; skipped", src))
		return nil
	}
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot read "+source))
	}

	info, err := p.fsys.Lstat(dest)
	switch {
	case isMissing(err):
		return p.addLink(target, source, dest, "Link "+target, false)
	case err != nil:
		return clierr.Wrap(err, clierr.Failed("cannot read "+dest))
	case info.Mode()&fs.ModeSymlink != 0:
		value, err := p.fsys.Readlink(dest)
		if err != nil {
			return clierr.Wrap(err, clierr.Failed("cannot read link "+dest))
		}
		if resolveLink(dest, value) == filepath.Clean(source) {
			return nil
		}
		return p.addLink(target, source, dest, "Relink "+target, true)
	case info.IsDir():
		p.conflict(target)
		return nil
	}

	same, err := p.sameBytes(dest, source, srcInfo)
	if err != nil {
		return err
	}
	if !same {
		p.conflict(target)
	}
	return nil
}

// resolveLink returns the cleaned absolute path a link value names. Relative values are relative to
// the directory of the link.
func resolveLink(dest, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(filepath.Dir(dest), value)
}

// sameBytes reports whether the regular file at dest holds the same bytes as source. A directory
// source is never the same bytes as a regular file.
func (p *planner) sameBytes(dest, source string, srcInfo fs.FileInfo) (bool, error) {
	if srcInfo.IsDir() {
		return false, nil
	}
	want, err := p.fsys.ReadFile(source)
	if err != nil {
		return false, clierr.Wrap(err, clierr.Failed("cannot read "+source))
	}
	got, err := p.fsys.ReadFile(dest)
	if err != nil {
		return false, clierr.Wrap(err, clierr.Failed("cannot read "+dest))
	}
	return string(got) == string(want), nil
}

// addLink plans a symlink at dest pointing at source, relative to dest's directory. relink removes
// the existing link first.
func (p *planner) addLink(target, source, dest, summary string, relink bool) error {
	rel, err := filepath.Rel(filepath.Dir(dest), source)
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot relate "+dest+" to "+source))
	}
	p.plan.Actions = append(p.plan.Actions, engine.Action{
		Part:    partName,
		Subject: target,
		Summary: summary,
		Do: func(context.Context) error {
			if relink {
				if err := p.fsys.Remove(dest); err != nil {
					return clierr.Wrap(err, clierr.Failed("cannot remove link "+dest))
				}
			}
			if err := p.fsys.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot create "+filepath.Dir(dest)))
			}
			if err := p.fsys.Symlink(rel, dest); err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot link "+dest))
			}
			return nil
		},
	})
	return nil
}

// file plans the regular file at target with the declared content. A missing file is written. A
// present file, or a symlink, whose content differs is never overwritten: it is reported as a
// warning, so a hand edit survives until its owner deletes it.
func (p *planner) file(target, content string) error {
	dest := filepath.Join(p.root, filepath.FromSlash(target))
	info, err := p.fsys.Lstat(dest)
	switch {
	case isMissing(err):
		p.addWrite(target, dest, content)
		return nil
	case err != nil:
		return clierr.Wrap(err, clierr.Failed("cannot read "+dest))
	case info.IsDir():
		p.conflict(target)
		return nil
	}
	got, err := p.fsys.ReadFile(dest)
	switch {
	case err == nil && string(got) == content:
		return nil
	case err != nil && !isMissing(err):
		return clierr.Wrap(err, clierr.Failed("cannot read "+dest))
	}
	// Different content, or a symlink that does not resolve to a readable file.
	p.finding(target, engine.SeverityWarn, fmt.Sprintf("%s differs from root.yaml; delete it to restore the declared content", target))
	return nil
}

// addWrite plans writing the declared content to a missing file at dest.
func (p *planner) addWrite(target, dest, content string) {
	p.plan.Actions = append(p.plan.Actions, engine.Action{
		Part:    partName,
		Subject: target,
		Summary: "Write " + target,
		Do: func(context.Context) error {
			if err := p.fsys.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot create "+filepath.Dir(dest)))
			}
			if err := p.fsys.WriteFile(dest, []byte(content), 0o644); err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot write "+dest))
			}
			return nil
		},
	})
}

// conflict records a hand-made file or directory at target. It is never changed.
func (p *planner) conflict(target string) {
	p.finding(target, engine.SeverityError, fmt.Sprintf("conflict: %s is hand-made; move it away and rerun", target))
}

func (p *planner) finding(subject string, sev engine.Severity, msg string) {
	p.plan.Findings = append(p.plan.Findings, engine.Finding{
		Part:     partName,
		Subject:  subject,
		Message:  msg,
		Severity: sev,
	})
}

// generatorState is the state file: the time each generator command last ran successfully.
type generatorState struct {
	Generators map[string]string `json:"generators"`
}

// planGenerators plans one run per generator command, in declaration order. A command runs when the
// root changed in this plan or when no run is recorded for it.
func (p part) planGenerators(s *engine.State, cmds []string, changed bool) ([]engine.Action, error) {
	path, err := p.statePath()
	if err != nil {
		return nil, err
	}
	state, err := readState(s.FS, path)
	if err != nil {
		return nil, err
	}
	var out []engine.Action
	for _, cmd := range cmds {
		if _, ran := state.Generators[cmd]; ran && !changed {
			continue
		}
		out = append(out, engine.Action{
			Part:    partName,
			Subject: cmd,
			Summary: "Run " + cmd,
			Do: func(ctx context.Context) error {
				res, err := s.Run.Run(ctx, s.Root, "sh", "-c", cmd)
				if err != nil {
					return clierr.Wrap(err, clierr.Tool(fmt.Sprintf("generator %s could not run:\n%s", cmd, outputTail(res))))
				}
				if res.ExitCode != 0 {
					return clierr.Tool(fmt.Sprintf("generator %s exited %d:\n%s", cmd, res.ExitCode, outputTail(res)))
				}
				return recordRun(s.FS, path, cmd, time.Now())
			},
		})
	}
	return out, nil
}

// statePath is the generator state file: the injected path, or $XDG_STATE_HOME/orbit/root.json with
// ~/.local/state as the default base.
func (p part) statePath() (string, error) {
	if p.state != "" {
		return p.state, nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", clierr.Wrap(err, clierr.Failed("cannot find the home directory"))
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, filepath.FromSlash(stateRel)), nil
}

// readState loads the state file. A missing or unparseable file is an empty state, so its generators
// run again and the next recordRun rewrites the file.
func readState(fsys engine.FS, path string) (generatorState, error) {
	var state generatorState
	data, err := fsys.ReadFile(path)
	if isMissing(err) {
		return generatorState{Generators: map[string]string{}}, nil
	}
	if err != nil {
		return generatorState{}, clierr.Wrap(err, clierr.Failed("cannot read generator state "+path))
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return generatorState{Generators: map[string]string{}}, nil
	}
	if state.Generators == nil {
		state.Generators = map[string]string{}
	}
	return state, nil
}

// recordRun writes the time cmd ran into the state file. The file is re-read first, so each
// generator action records itself without losing the others.
func recordRun(fsys engine.FS, path, cmd string, at time.Time) error {
	state, err := readState(fsys, path)
	if err != nil {
		return err
	}
	state.Generators[cmd] = at.UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot encode generator state"))
	}
	if err := fsys.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot create "+filepath.Dir(path)))
	}
	if err := fsys.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot write generator state "+path))
	}
	return nil
}

// outputTail returns the last 20 lines of a command's stdout and stderr, combined.
func outputTail(res runner.Result) string {
	text := strings.TrimRight(res.Stdout+"\n"+res.Stderr, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}

// isMissing reports whether err means the path does not exist, including a parent that is a file.
func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
