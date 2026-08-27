package env

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// VarType represents the data type of an environment variable in a schema.
type VarType string

const (
	TypeString  VarType = "string"
	TypeInteger VarType = "integer"
	TypeURL     VarType = "url"
	TypeSecret  VarType = "secret"
	TypeBoolean VarType = "boolean"
)

// VariableDef defines an environment variable specification within .env.schema.yaml.
type VariableDef struct {
	Name        string  `yaml:"name" json:"name"`
	Type        VarType `yaml:"type" json:"type"`
	Default     string  `yaml:"default,omitempty" json:"default,omitempty"`
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool    `yaml:"required,omitempty" json:"required,omitempty"`
	Generator   string  `yaml:"generator,omitempty" json:"generator,omitempty"`
}

// UnmarshalYAML implements custom unmarshaling for VariableDef to handle default values
// of different types (int, bool, string) seamlessly.
func (v *VariableDef) UnmarshalYAML(node *yaml.Node) error {
	type rawVar struct {
		Name        string      `yaml:"name"`
		Type        VarType     `yaml:"type"`
		Default     interface{} `yaml:"default"`
		Description string      `yaml:"description"`
		Required    bool        `yaml:"required"`
		Generator   string      `yaml:"generator"`
	}

	var raw rawVar
	if err := node.Decode(&raw); err != nil {
		return err
	}

	v.Name = raw.Name
	v.Type = raw.Type
	v.Description = raw.Description
	v.Required = raw.Required
	v.Generator = raw.Generator

	if raw.Default != nil {
		switch val := raw.Default.(type) {
		case string:
			v.Default = val
		case int:
			v.Default = fmt.Sprintf("%d", val)
		case int64:
			v.Default = fmt.Sprintf("%d", val)
		case float64:
			if val == float64(int64(val)) {
				v.Default = fmt.Sprintf("%d", int64(val))
			} else {
				v.Default = fmt.Sprintf("%g", val)
			}
		case bool:
			v.Default = fmt.Sprintf("%t", val)
		default:
			v.Default = fmt.Sprintf("%v", val)
		}
	}

	return nil
}

// EnvSchema defines the structure of a .env.schema.yaml file.
type EnvSchema struct {
	Version   string        `yaml:"version" json:"version"`
	Variables []VariableDef `yaml:"variables" json:"variables"`
}

// ValidationError represents an issue found during environment validation.
type ValidationError struct {
	Variable string `json:"variable" yaml:"variable"`
	Message  string `json:"message" yaml:"message"`
	Type     string `json:"type" yaml:"type"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("variable %s: %s (%s)", e.Variable, e.Message, e.Type)
}
