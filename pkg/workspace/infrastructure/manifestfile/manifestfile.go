// Package manifestfile implements the workspace manifest port with pkg/manifest.
package manifestfile

import "github.com/manovaspace/orbit-cli/pkg/manifest"

// Loader reads workspace manifests from disk.
type Loader struct{}

// New builds a Loader.
func New() *Loader { return &Loader{} }

// Load parses the manifest at path.
func (l *Loader) Load(path string) (*manifest.WorkspaceManifest, error) {
	return manifest.Load(path)
}
