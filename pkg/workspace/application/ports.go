// Package application implements the workspace use cases over git, filesystem
// and manifest ports so the CLI commands stay thin and the logic is testable
// without external binaries.
package application

import (
	"context"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

// Git is the local git checkout the workspace commands inspect, clone and
// update. Every method runs one or more external commands through the adapter.
type Git interface {
	// IsWorkTree reports whether dir is inside a git working tree.
	IsWorkTree(ctx context.Context, dir string) (bool, error)
	// Branch returns the current branch, falling back to a symbolic ref or
	// "HEAD" exactly as the old command did.
	Branch(ctx context.Context, dir string) (string, error)
	// Status returns the raw `git status --porcelain` output.
	Status(ctx context.Context, dir string) (string, error)
	// AheadBehind returns the left/right counts against the upstream.
	AheadBehind(ctx context.Context, dir string) (ahead, behind int, err error)
	Fetch(ctx context.Context, dir string) error
	// MergeFFOnly merges ref into the current branch and returns git's output.
	MergeFFOnly(ctx context.Context, dir, ref string) (string, error)
	// Clone clones remoteURL into dest. A non-empty dir runs from that working
	// directory; dest may be relative to it.
	Clone(ctx context.Context, dir, remoteURL, dest string) (string, error)
}

// FileInfo is the subset of os.FileInfo the use cases need.
type FileInfo struct {
	IsDir bool
}

// FS is the filesystem the workspace use cases touch. Stat reports a missing
// path as exists=false with a nil error.
type FS interface {
	Stat(path string) (info FileInfo, exists bool, err error)
	ReadDir(path string) ([]string, error)
	MkdirAll(path string) error
	TempDir(pattern string) (string, error)
	RemoveAll(path string) error
	// CopyTree copies src recursively into dst, creating dst.
	CopyTree(src, dst string) error
}

// Manifest loads workspace.yaml. The adapter delegates to pkg/manifest.
type Manifest interface {
	Load(path string) (*manifest.WorkspaceManifest, error)
}

// UI carries the decoration cmd/orbit already renders. Keeping it injected lets
// this package write the same progress lines without importing a styling
// library.
type UI struct {
	OK    string
	Warn  string
	Error string
	Info  string

	Title     func(string) string
	Header    func(string) string
	Bold      func(string) string
	Success   func(string) string
	Warning   func(string) string
	ErrorText func(string) string
	InfoText  func(string) string
	Subtle    func(string) string
}

// Service is the workspace use-case layer.
type Service struct {
	Git      Git
	FS       FS
	Manifest Manifest
	Out      io.Writer
	UI       UI
}
