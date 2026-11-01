// Package env keeps the schema contract helpers used by commands outside the
// env command group. The implementation lives in the layered subpackages.
package env

import (
	"github.com/manovaspace/orbit-cli/pkg/env/application"
	"github.com/manovaspace/orbit-cli/pkg/env/domain"
	"github.com/manovaspace/orbit-cli/pkg/env/infrastructure/osfs"
	"github.com/manovaspace/orbit-cli/pkg/env/infrastructure/schemafile"
)

// EnvSchema is a parsed .env.schema.yaml contract.
type EnvSchema = domain.EnvSchema

// VariableDef is one variable declaration inside a contract.
type VariableDef = domain.VariableDef

// VarType is the declared data type of a variable.
type VarType = domain.VarType

// ValidationError is one issue found while validating an .env file.
type ValidationError = domain.ValidationError

// FindSchemas walks root for .env.schema.yaml contracts.
func FindSchemas(root string) ([]string, error) {
	return osfs.New().Find(root)
}

// LoadSchema reads and parses a .env.schema.yaml contract.
func LoadSchema(path string) (*EnvSchema, error) {
	return schemafile.New().Load(path)
}

// Validate checks an .env file against a contract and returns its findings.
func Validate(envPath string, schema *EnvSchema) []ValidationError {
	return application.ValidateFile(osfs.New(), envPath, schema)
}
