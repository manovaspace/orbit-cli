// Package domain holds the gitignored-asset index types and rules. It performs
// no I/O.
package domain

import (
	"errors"
	"path/filepath"
	"strings"
)

// ManifestFile is the per-repository index of gitignored assets.
const ManifestFile = "orbit-assets.yaml"

const warnUnderBytes = 100 * 1024

var (
	// ErrNotFound reports that an object is absent from the store.
	ErrNotFound = errors.New("asset object not found")
	// ErrHashMismatch reports a local file that differs from the index.
	ErrHashMismatch = errors.New("local file hash does not match orbit-assets.yaml")
)

// Object is one asset listed in orbit-assets.yaml.
type Object struct {
	Path        string `json:"path" yaml:"path"`
	SHA256      string `json:"sha256" yaml:"sha256"`
	Size        int64  `json:"size" yaml:"size"`
	ContentType string `json:"contentType,omitempty" yaml:"content_type,omitempty"`
}

// Manifest is the orbit-assets.yaml document.
type Manifest struct {
	Version int      `json:"version" yaml:"version"`
	Objects []Object `json:"objects" yaml:"objects"`
}

// Find returns the object at path.
func (m *Manifest) Find(path string) (Object, bool) {
	if m == nil {
		return Object{}, false
	}
	for _, o := range m.Objects {
		if o.Path == path {
			return o, true
		}
	}
	return Object{}, false
}

// Upsert inserts obj or replaces the object with the same path.
func (m *Manifest) Upsert(obj Object) {
	if m.Version == 0 {
		m.Version = 1
	}
	for i, o := range m.Objects {
		if o.Path == obj.Path {
			m.Objects[i] = obj
			return
		}
	}
	m.Objects = append(m.Objects, obj)
}

// FileState is the local state of one indexed asset.
type FileState string

const (
	FileOK       FileState = "ok"
	FileMissing  FileState = "missing"
	FileMismatch FileState = "mismatch"
)

// FileStatus is the status of one indexed asset.
type FileStatus struct {
	Path  string    `json:"path"`
	State FileState `json:"state"`
	Error string    `json:"error,omitempty"`
}

// ContentTypeFor returns the MIME type stored with an asset path.
func ContentTypeFor(path string) string {
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

// WarnSmall reports whether size is small enough to belong in git.
func WarnSmall(size int64) bool {
	return size < warnUnderBytes
}
