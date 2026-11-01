// Package schemafile loads .env.schema.yaml contracts from disk.
package schemafile

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/env/domain"
)

// Loader reads schema contracts from disk.
type Loader struct{}

// New builds a Loader.
func New() *Loader { return &Loader{} }

// Load reads and parses the schema at path.
func (l *Loader) Load(path string) (*domain.EnvSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}
	schema, err := domain.ParseSchema(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema from %s: %w", path, err)
	}
	return schema, nil
}
