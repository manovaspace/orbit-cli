package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const warnUnderBytes = 100 * 1024

func Add(ctx context.Context, repoRoot, relPath string, store Store) (Object, error) {
	relPath = filepath.ToSlash(relPath)
	abs := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	sum, size, err := HashFile(abs)
	if err != nil {
		return Object{}, err
	}
	ct := contentTypeFor(relPath)
	exists, err := store.Exists(ctx, sum)
	if err != nil {
		return Object{}, err
	}
	if !exists {
		f, err := os.Open(abs)
		if err != nil {
			return Object{}, err
		}
		err = store.Put(ctx, sum, f, size, ct, relPath)
		_ = f.Close()
		if err != nil {
			return Object{}, err
		}
	}
	obj := Object{Path: relPath, SHA256: sum, Size: size, ContentType: ct}
	m, err := Load(repoRoot)
	if err != nil {
		return Object{}, err
	}
	m.Upsert(obj)
	if err := Save(repoRoot, m); err != nil {
		return Object{}, err
	}
	if err := EnsureGitignore(repoRoot, relPath); err != nil {
		return Object{}, err
	}
	return obj, nil
}

func Pull(ctx context.Context, repoRoot string, store Store, opts PullOptions) error {
	m, err := Load(repoRoot)
	if err != nil {
		return err
	}
	var first error
	for _, obj := range m.Objects {
		if err := pullOne(ctx, repoRoot, store, obj, opts.Force); err != nil {
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func pullOne(ctx context.Context, repoRoot string, store Store, obj Object, force bool) error {
	abs := filepath.Join(repoRoot, filepath.FromSlash(obj.Path))
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		sum, _, err := HashFile(abs)
		if err != nil {
			return err
		}
		if strings.EqualFold(sum, obj.SHA256) {
			return nil
		}
		if !force {
			return fmt.Errorf("%s: %w", obj.Path, ErrHashMismatch)
		}
	}
	rc, err := store.Get(ctx, obj.SHA256)
	if err != nil {
		return fmt.Errorf("%s: %w", obj.Path, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	got := hex.EncodeToString(sha256Sum(data))
	if !strings.EqualFold(got, obj.SHA256) {
		return fmt.Errorf("%s: downloaded hash %s != %s", obj.Path, got, obj.SHA256)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	tmp := abs + ".orbit-download"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func Push(ctx context.Context, repoRoot string, store Store) error {
	m, err := Load(repoRoot)
	if err != nil {
		return err
	}
	for _, obj := range m.Objects {
		ok, err := store.Exists(ctx, obj.SHA256)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		abs := filepath.Join(repoRoot, filepath.FromSlash(obj.Path))
		f, err := os.Open(abs)
		if err != nil {
			return fmt.Errorf("push %s: %w", obj.Path, err)
		}
		err = store.Put(ctx, obj.SHA256, f, obj.Size, obj.ContentType, obj.Path)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func Status(repoRoot string) ([]FileStatus, error) {
	m, err := Load(repoRoot)
	if err != nil {
		return nil, err
	}
	out := make([]FileStatus, 0, len(m.Objects))
	for _, obj := range m.Objects {
		abs := filepath.Join(repoRoot, filepath.FromSlash(obj.Path))
		st, err := os.Stat(abs)
		if err != nil {
			out = append(out, FileStatus{Path: obj.Path, State: FileMissing})
			continue
		}
		if st.IsDir() {
			out = append(out, FileStatus{Path: obj.Path, State: FileMissing, Error: "is a directory"})
			continue
		}
		sum, _, err := HashFile(abs)
		if err != nil {
			out = append(out, FileStatus{Path: obj.Path, State: FileMismatch, Error: err.Error()})
			continue
		}
		if !strings.EqualFold(sum, obj.SHA256) {
			out = append(out, FileStatus{Path: obj.Path, State: FileMismatch})
			continue
		}
		out = append(out, FileStatus{Path: obj.Path, State: FileOK})
	}
	return out, nil
}

func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

func WarnSmall(size int64) bool {
	return size < warnUnderBytes
}
