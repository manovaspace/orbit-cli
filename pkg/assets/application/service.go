package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/assets/domain"
	"gopkg.in/yaml.v3"
)

// Pull downloads missing or outdated assets for relPaths under workspaceRoot.
func (s *Service) Pull(ctx context.Context, workspaceRoot string, relPaths []string, opts PullOptions) (PullResult, error) {
	result := PullResult{Repositories: []string{}}
	var first error
	for _, rel := range relPaths {
		root := filepath.Join(workspaceRoot, rel)
		has, err := s.HasManifest(root)
		if err != nil {
			return result, err
		}
		if !has {
			continue
		}
		result.Repositories = append(result.Repositories, rel)
		if err := s.pullRepo(ctx, root, opts); err != nil {
			if first == nil {
				first = fmt.Errorf("%s: %w", rel, err)
			}
		}
	}
	if first != nil {
		return result, first
	}
	fmt.Fprintln(s.out(), "Assets pulled.")
	return result, nil
}

// Push uploads index objects that are missing from the store.
func (s *Service) Push(ctx context.Context, workspaceRoot string, relPaths []string) (PushResult, error) {
	result := PushResult{Repositories: []string{}}
	var first error
	for _, rel := range relPaths {
		root := filepath.Join(workspaceRoot, rel)
		has, err := s.HasManifest(root)
		if err != nil {
			return result, err
		}
		if !has {
			continue
		}
		result.Repositories = append(result.Repositories, rel)
		if err := s.pushRepo(ctx, root); err != nil {
			if first == nil {
				first = fmt.Errorf("%s: %w", rel, err)
			}
		}
	}
	if first != nil {
		return result, first
	}
	fmt.Fprintln(s.out(), "Assets pushed.")
	return result, nil
}

// Status reports the local state of every indexed asset and renders the same
// lines `orbit assets status` has always printed.
func (s *Service) Status(_ context.Context, workspaceRoot string, relPaths []string) (StatusResult, error) {
	result := StatusResult{Repositories: []StatusRepository{}}
	out := s.out()
	for _, rel := range relPaths {
		root := filepath.Join(workspaceRoot, rel)
		has, err := s.HasManifest(root)
		if err != nil {
			return result, err
		}
		if !has {
			continue
		}
		files, err := s.statusRepo(root)
		if err != nil {
			return result, err
		}
		result.Repositories = append(result.Repositories, StatusRepository{Path: rel, Files: files})
		fmt.Fprintf(out, "%s\n", rel)
		for _, file := range files {
			mark := "ok"
			if file.State != domain.FileOK {
				result.Problems++
				mark = string(file.State)
			}
			fmt.Fprintf(out, "  %s  %s\n", mark, file.Path)
		}
	}
	return result, nil
}

// Add hashes, uploads and gitignores relPath, then records it in the manifest.
func (s *Service) Add(ctx context.Context, repoRoot, relPath string) (AddResult, error) {
	relPath = filepath.ToSlash(relPath)
	abs := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	sum, size, err := s.FS.HashFile(abs)
	if err != nil {
		return AddResult{}, err
	}
	contentType := domain.ContentTypeFor(relPath)
	exists, err := s.R2.Exists(ctx, sum)
	if err != nil {
		return AddResult{}, err
	}
	if !exists {
		f, err := s.FS.Open(abs)
		if err != nil {
			return AddResult{}, err
		}
		err = s.R2.Put(ctx, sum, f, size, contentType, relPath)
		_ = f.Close()
		if err != nil {
			return AddResult{}, err
		}
	}
	obj := domain.Object{Path: relPath, SHA256: sum, Size: size, ContentType: contentType}
	m, err := s.LoadManifest(repoRoot)
	if err != nil {
		return AddResult{}, err
	}
	m.Upsert(obj)
	if err := s.SaveManifest(repoRoot, m); err != nil {
		return AddResult{}, err
	}
	if err := s.ensureGitignore(repoRoot, relPath); err != nil {
		return AddResult{}, err
	}
	return AddResult{Object: obj, Small: domain.WarnSmall(obj.Size)}, nil
}

// HasManifest reports whether repoRoot has an orbit-assets.yaml index.
func (s *Service) HasManifest(repoRoot string) (bool, error) {
	_, exists, err := s.FS.Stat(filepath.Join(repoRoot, domain.ManifestFile))
	return exists, err
}

// LoadManifest reads the index of repoRoot, defaulting to an empty v1 index.
func (s *Service) LoadManifest(repoRoot string) (domain.Manifest, error) {
	path := filepath.Join(repoRoot, domain.ManifestFile)
	data, err := s.FS.ReadFile(path)
	if err != nil {
		if !s.exists(path) {
			return domain.Manifest{Version: 1, Objects: []domain.Object{}}, nil
		}
		return domain.Manifest{}, fmt.Errorf("read %s: %w", path, err)
	}
	var m domain.Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return domain.Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Objects == nil {
		m.Objects = []domain.Object{}
	}
	return m, nil
}

// SaveManifest writes the index of repoRoot.
func (s *Service) SaveManifest(repoRoot string, m domain.Manifest) error {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Objects == nil {
		m.Objects = []domain.Object{}
	}
	data, err := yaml.Marshal(&m)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", domain.ManifestFile, err)
	}
	if err := s.FS.WriteFile(filepath.Join(repoRoot, domain.ManifestFile), data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", domain.ManifestFile, err)
	}
	return nil
}

func (s *Service) pullRepo(ctx context.Context, repoRoot string, opts PullOptions) error {
	m, err := s.LoadManifest(repoRoot)
	if err != nil {
		return err
	}
	var first error
	for _, obj := range m.Objects {
		if err := s.pullOne(ctx, repoRoot, obj, opts.Force); err != nil {
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *Service) pullOne(ctx context.Context, repoRoot string, obj domain.Object, force bool) error {
	abs := filepath.Join(repoRoot, filepath.FromSlash(obj.Path))
	info, exists, err := s.FS.Stat(abs)
	if err != nil {
		return err
	}
	if exists && !info.IsDir {
		sum, _, err := s.FS.HashFile(abs)
		if err != nil {
			return err
		}
		if strings.EqualFold(sum, obj.SHA256) {
			return nil
		}
		if !force {
			return fmt.Errorf("%s: %w", obj.Path, domain.ErrHashMismatch)
		}
	}
	rc, err := s.R2.Get(ctx, obj.SHA256)
	if err != nil {
		return fmt.Errorf("%s: %w", obj.Path, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	got := hex.EncodeToString(sha256Sum(data))
	if !strings.EqualFold(got, obj.SHA256) {
		return fmt.Errorf("%s: downloaded hash %s != %s", obj.Path, got, obj.SHA256)
	}
	if err := s.FS.MkdirAll(filepath.Dir(abs)); err != nil {
		return err
	}
	tmp := abs + ".orbit-download"
	if err := s.FS.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := s.FS.Rename(tmp, abs); err != nil {
		_ = s.FS.Remove(tmp)
		return err
	}
	return nil
}

func (s *Service) pushRepo(ctx context.Context, repoRoot string) error {
	m, err := s.LoadManifest(repoRoot)
	if err != nil {
		return err
	}
	for _, obj := range m.Objects {
		ok, err := s.R2.Exists(ctx, obj.SHA256)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		abs := filepath.Join(repoRoot, filepath.FromSlash(obj.Path))
		f, err := s.FS.Open(abs)
		if err != nil {
			return fmt.Errorf("push %s: %w", obj.Path, err)
		}
		err = s.R2.Put(ctx, obj.SHA256, f, obj.Size, obj.ContentType, obj.Path)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) statusRepo(repoRoot string) ([]domain.FileStatus, error) {
	m, err := s.LoadManifest(repoRoot)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FileStatus, 0, len(m.Objects))
	for _, obj := range m.Objects {
		abs := filepath.Join(repoRoot, filepath.FromSlash(obj.Path))
		info, exists, err := s.FS.Stat(abs)
		if err != nil {
			out = append(out, domain.FileStatus{Path: obj.Path, State: domain.FileMismatch, Error: err.Error()})
			continue
		}
		if !exists {
			out = append(out, domain.FileStatus{Path: obj.Path, State: domain.FileMissing})
			continue
		}
		if info.IsDir {
			out = append(out, domain.FileStatus{Path: obj.Path, State: domain.FileMissing, Error: "is a directory"})
			continue
		}
		sum, _, err := s.FS.HashFile(abs)
		if err != nil {
			out = append(out, domain.FileStatus{Path: obj.Path, State: domain.FileMismatch, Error: err.Error()})
			continue
		}
		if !strings.EqualFold(sum, obj.SHA256) {
			out = append(out, domain.FileStatus{Path: obj.Path, State: domain.FileMismatch})
			continue
		}
		out = append(out, domain.FileStatus{Path: obj.Path, State: domain.FileOK})
	}
	return out, nil
}

func (s *Service) ensureGitignore(repoRoot, relPath string) error {
	relPath = filepath.ToSlash(relPath)
	path := filepath.Join(repoRoot, ".gitignore")

	data, readErr := s.FS.ReadFile(path)
	existing := map[string]struct{}{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			existing[line] = struct{}{}
		}
	}
	if _, ok := existing[relPath]; ok {
		return nil
	}

	if readErr != nil && !s.exists(path) {
		body := "# orbit-assets — synced by `orbit assets`, not git\n" + relPath + "\n"
		return s.FS.WriteFile(path, []byte(body), 0o644)
	}
	if readErr != nil {
		return fmt.Errorf("read .gitignore: %w", readErr)
	}

	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += relPath + "\n"
	if err := s.FS.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("open .gitignore: %w", err)
	}
	return nil
}

func (s *Service) exists(path string) bool {
	_, exists, err := s.FS.Stat(path)
	return err == nil && exists
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
