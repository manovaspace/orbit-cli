// Package handbook clones the staff handbook into the workspace. The handbook holds the workspace
// manifest, which the engine reloads after the clone.
package handbook

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	partName       = "handbook"
	notCheckout    = "handbook/ exists but is not a git checkout: move it aside and rerun o setup"
	handbookGitSSH = "git@github.com:" + spec.HandbookRepo + ".git"
)

type part struct{}

// New returns the handbook part. It needs identity, so git can reach GitHub when it clones.
func New() engine.Part { return part{} }

func (part) Name() string    { return partName }
func (part) Needs() []string { return []string{"identity"} }

// Plan clones the handbook when <Root>/handbook has no .git/HEAD. A missing directory, or one
// holding nothing but an interrupted .git, is cloned fresh. Any other existing directory is a
// finding: the part never deletes user files.
func (part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	dir := filepath.Join(s.Root, spec.HandbookPath)
	if _, err := s.FS.Stat(filepath.Join(dir, ".git", "HEAD")); err == nil {
		return engine.Plan{}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return engine.Plan{}, clierr.Wrap(err, clierr.Failed("cannot read "+dir))
	}

	_, err := s.FS.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return cloneAction(s, dir, false)
	}
	if err != nil {
		return engine.Plan{}, clierr.Wrap(err, clierr.Failed("cannot read "+dir))
	}

	entries, err := s.FS.ReadDir(dir)
	if err != nil || !onlyInterruptedClone(entries) {
		return engine.Plan{Findings: []engine.Finding{{
			Part:     partName,
			Subject:  spec.HandbookPath,
			Message:  notCheckout,
			Severity: engine.SeverityError,
		}}}, nil
	}
	return cloneAction(s, dir, true)
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

// cloneAction plans git clone --filter=blob:none into dir, after removing a partial dir when
// remove is set. Do reloads the state, so the manifest is read once the clone exists.
func cloneAction(s *engine.State, dir string, remove bool) (engine.Plan, error) {
	url, err := spec.CloneURL(handbookGitSSH, s.Machine.GitProtocol)
	if err != nil {
		return engine.Plan{}, err
	}
	return engine.Plan{Actions: []engine.Action{{
		Part:    partName,
		Subject: spec.HandbookPath,
		Summary: "Clone handbook",
		Do: func(ctx context.Context) error {
			if remove {
				if err := s.FS.RemoveAll(dir); err != nil {
					return clierr.Wrap(err, clierr.Failed("cannot remove partial "+dir))
				}
			}
			if err := s.FS.MkdirAll(s.Root, 0o755); err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot create "+s.Root))
			}
			if _, err := s.Run.Run(ctx, "", "git", "clone", "--filter=blob:none", url, dir); err != nil {
				return err
			}
			return s.Reload()
		},
	}}}, nil
}
