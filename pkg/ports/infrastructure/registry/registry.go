// Package registry loads handbook/agents/workspace/ports.yaml, the dev port
// registry (ADR-058), into the domain types.
package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/ports/domain"
	"gopkg.in/yaml.v3"
)

// RelPath is the registry location relative to the workspace root.
const RelPath = "handbook/agents/workspace/ports.yaml"

// ErrNotFound reports that no registry file exists. Callers keep their
// pre-registry behaviour for it; every other load error is a real failure.
var ErrNotFound = errors.New("port registry not found")

// Find loads the registry under workspaceRoot. An empty root is ErrNotFound.
func Find(workspaceRoot string) (*domain.Registry, error) {
	if workspaceRoot == "" {
		return nil, fmt.Errorf("no workspace root: %w", ErrNotFound)
	}
	return Load(filepath.Join(workspaceRoot, filepath.FromSlash(RelPath)))
}

// Load reads and validates the registry at path. A missing file wraps
// ErrNotFound.
func Load(path string) (*domain.Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	reg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return reg, nil
}

// Parse decodes registry YAML and rejects a port number claimed twice.
func Parse(data []byte) (*domain.Registry, error) {
	var reg domain.Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse ports registry: %w", err)
	}
	seen := make(map[int]string)
	claim := func(port int, what string) error {
		if prev, ok := seen[port]; ok {
			return fmt.Errorf("port %d is listed twice (%s and %s)", port, prev, what)
		}
		seen[port] = what
		return nil
	}
	for _, s := range reg.Services {
		if err := claim(s.Port, "service "+s.Name); err != nil {
			return nil, err
		}
	}
	for _, r := range reg.Reserved {
		if err := claim(r.Port, "reserved "+r.Name); err != nil {
			return nil, err
		}
	}
	for _, e := range reg.Exceptions {
		if err := claim(e.Port, "exception "+e.Name); err != nil {
			return nil, err
		}
	}
	for _, r := range reg.Retired {
		if err := claim(r.Port, "retired "+r.Was); err != nil {
			return nil, err
		}
	}
	return &reg, nil
}
