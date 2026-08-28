package assets

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound     = errors.New("asset object not found")
	ErrHashMismatch = errors.New("local file hash does not match orbit-assets.yaml")
)

type Store interface {
	Put(ctx context.Context, sha256 string, r io.Reader, size int64, contentType, sourcePath string) error
	Get(ctx context.Context, sha256 string) (io.ReadCloser, error)
	Exists(ctx context.Context, sha256 string) (bool, error)
}

type PullOptions struct {
	Force bool
}

type FileState string

const (
	FileOK       FileState = "ok"
	FileMissing  FileState = "missing"
	FileMismatch FileState = "mismatch"
)

type FileStatus struct {
	Path  string
	State FileState
	Error string
}
