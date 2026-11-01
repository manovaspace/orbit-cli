// Package application implements the asset sync use cases over the object store
// and filesystem ports, keeping the CLI commands thin.
package application

import (
	"context"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/assets/domain"
)

// Store is the private object store that holds asset bytes keyed by SHA-256.
type Store interface {
	Put(ctx context.Context, sha256 string, r io.Reader, size int64, contentType, sourcePath string) error
	Get(ctx context.Context, sha256 string) (io.ReadCloser, error)
	Exists(ctx context.Context, sha256 string) (bool, error)
}

// FileInfo is the subset of file metadata the use cases need.
type FileInfo struct {
	IsDir bool
}

// FS is the repository filesystem the use cases read and write.
type FS interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm uint32) error
	MkdirAll(path string) error
	Rename(oldPath, newPath string) error
	Remove(path string) error
	// Stat reports a missing path as exists=false with a nil error.
	Stat(path string) (info FileInfo, exists bool, err error)
	Open(path string) (io.ReadCloser, error)
	// HashFile returns the lowercase hex SHA-256 and the size of path.
	HashFile(path string) (sum string, size int64, err error)
}

// Service is the asset use-case layer.
type Service struct {
	FS  FS
	R2  Store
	Out io.Writer
}

// PullOptions selects overwriting behaviour for `assets pull`.
type PullOptions struct {
	Force bool
}

// PullResult is what `assets pull` processed.
type PullResult struct {
	Repositories []string `json:"repositories"`
}

// PushResult is what `assets push` processed.
type PushResult struct {
	Repositories []string `json:"repositories"`
}

// StatusRepository is one repository's asset status.
type StatusRepository struct {
	Path  string              `json:"path"`
	Files []domain.FileStatus `json:"files"`
}

// StatusResult is what `assets status` found.
type StatusResult struct {
	Repositories []StatusRepository `json:"repositories"`
	Problems     int                `json:"problems"`
}

// AddResult is what `assets add` produced.
type AddResult struct {
	Object domain.Object `json:"object"`
	Small  bool          `json:"small"`
}
