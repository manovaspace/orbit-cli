// Package application implements the configuration use cases over a file store
// and an environment port, keeping the CLI commands thin.
package application

import (
	"io"

	"github.com/manovaspace/orbit-cli/pkg/config/domain"
)

// Store reads and writes the configuration file. Paths are resolved by the
// adapter, so an empty path means the default configuration file.
type Store interface {
	// DefaultPath is the user configuration file used when a path is empty.
	DefaultPath() string
	// Read returns the file bytes. A missing file returns fs.ErrNotExist.
	Read(path string) ([]byte, error)
	Load(path string) (*domain.Config, error)
	Save(path string, cfg *domain.Config) error
	// Exists reports a missing path as exists=false with a nil error.
	Exists(path string) (bool, error)
	SetInFile(path, key, value string) error
	UnsetInFile(path, key string) error
}

// Environment supplies the variables configuration precedence reads.
type Environment interface {
	Getenv(key string) string
}

// UI carries the decoration cmd/orbit already renders.
type UI struct {
	OK   string
	Warn string
	Info string

	Bold   func(string) string
	Subtle func(string) string
	Code   func(string) string
}

// Service is the configuration use-case layer.
type Service struct {
	Store Store
	Env   Environment
	Out   io.Writer
	UI    UI
}

// ResolveOptions are the explicit CLI flags layered above env and file values.
type ResolveOptions struct {
	ConfigPath       string
	ServerFlag       string
	StaffURLFlag     string
	AssetsBucketFlag string
	ScopeFlag        string
}

// InitResult is what `config init` produced.
type InitResult struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Created bool   `json:"created"`
}

// SetResult is what `config set` produced.
type SetResult struct {
	Key           string `json:"key"`
	Value         string `json:"value"`
	Path          string `json:"path"`
	SecretWarning bool   `json:"secretWarning"`
}

// UnsetResult is what `config unset` produced.
type UnsetResult struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}
