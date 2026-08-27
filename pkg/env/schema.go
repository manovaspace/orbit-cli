package env

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadSchema reads and parses a .env.schema.yaml file from the specified path.
func LoadSchema(path string) (*EnvSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	schema, err := ParseSchema(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema from %s: %w", path, err)
	}

	return schema, nil
}

// ParseSchema parses YAML bytes into an EnvSchema.
func ParseSchema(data []byte) (*EnvSchema, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("schema content is empty")
	}

	var schema EnvSchema
	if err := yaml.Unmarshal(trimmed, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema YAML: %w", err)
	}

	if schema.Version == "" {
		return nil, fmt.Errorf("missing required schema version")
	}

	return &schema, nil
}

// GetVariable returns the VariableDef by name from the schema, or nil if not found.
func (s *EnvSchema) GetVariable(name string) *VariableDef {
	if s == nil {
		return nil
	}
	for i := range s.Variables {
		if s.Variables[i].Name == name {
			return &s.Variables[i]
		}
	}
	return nil
}

// RequiredVariables returns a slice of all variables marked as required in the schema.
func (s *EnvSchema) RequiredVariables() []VariableDef {
	if s == nil {
		return nil
	}
	var required []VariableDef
	for _, v := range s.Variables {
		if v.Required {
			required = append(required, v)
		}
	}
	return required
}
