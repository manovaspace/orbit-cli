package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSchemaValid(t *testing.T) {
	content := `
version: "1"
variables:
  - name: ORBIT_AUTH_PORT
    type: integer
    default: 10051
    description: "gRPC service port"
    required: true

  - name: DATABASE_URL
    type: url
    default: "postgres://orbit:orbit@localhost:10000/orbit_auth?sslmode=disable"
    description: "Local dev Postgres connection string"
    required: true

  - name: FORGEJO_ACCESS_TOKEN
    type: secret
    description: "Personal access token for git.dev.manova.space"
    required: true
    generator: "interactive"

  - name: ENABLE_FEATURE_X
    type: boolean
    default: true
    description: "Feature flag"
    required: false

  - name: APP_ENV
    type: string
    default: "development"
    description: "Environment mode"
    required: false
`
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, ".env.schema.yaml")
	if err := os.WriteFile(schemaPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatalf("unexpected error loading schema: %v", err)
	}

	if schema.Version != "1" {
		t.Errorf("expected version '1', got %s", schema.Version)
	}

	if len(schema.Variables) != 5 {
		t.Fatalf("expected 5 variables, got %d", len(schema.Variables))
	}

	portVar := schema.GetVariable("ORBIT_AUTH_PORT")
	if portVar == nil {
		t.Fatal("expected ORBIT_AUTH_PORT variable to be found")
	}
	if portVar.Type != TypeInteger {
		t.Errorf("expected TypeInteger, got %s", portVar.Type)
	}
	if portVar.Default != "10051" {
		t.Errorf("expected default '10051', got %s", portVar.Default)
	}
	if !portVar.Required {
		t.Errorf("expected ORBIT_AUTH_PORT to be required")
	}

	boolVar := schema.GetVariable("ENABLE_FEATURE_X")
	if boolVar == nil {
		t.Fatal("expected ENABLE_FEATURE_X variable to be found")
	}
	if boolVar.Type != TypeBoolean {
		t.Errorf("expected TypeBoolean, got %s", boolVar.Type)
	}
	if boolVar.Default != "true" {
		t.Errorf("expected default 'true', got %s", boolVar.Default)
	}

	reqVars := schema.RequiredVariables()
	if len(reqVars) != 3 {
		t.Errorf("expected 3 required variables, got %d", len(reqVars))
	}

	nonExistent := schema.GetVariable("NON_EXISTENT")
	if nonExistent != nil {
		t.Errorf("expected nil for non-existent variable, got %+v", nonExistent)
	}
}

func TestLoadSchemaInvalid(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Non-existent file
	_, err := LoadSchema(filepath.Join(tmpDir, "does_not_exist.yaml"))
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}

	// 2. Empty file
	emptyFile := filepath.Join(tmpDir, "empty.yaml")
	_ = os.WriteFile(emptyFile, []byte("   \n"), 0644)
	_, err = LoadSchema(emptyFile)
	if err == nil {
		t.Error("expected error for empty file, got nil")
	}

	// 3. Malformed YAML
	malformedFile := filepath.Join(tmpDir, "malformed.yaml")
	_ = os.WriteFile(malformedFile, []byte("version: [unclosed\n"), 0644)
	_, err = LoadSchema(malformedFile)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}

	// 4. Missing version
	noVersionFile := filepath.Join(tmpDir, "no_version.yaml")
	_ = os.WriteFile(noVersionFile, []byte("variables:\n  - name: FOO\n"), 0644)
	_, err = LoadSchema(noVersionFile)
	if err == nil {
		t.Error("expected error for missing version, got nil")
	}
}

func TestParseEnvContent(t *testing.T) {
	envText := `
# System configuration
export APP_NAME="My Application"
PORT=8080 # default port
DATABASE_URL='postgres://user:pass@localhost:5432/db'

# Escaped values
SECRET="key_with_\"quotes\"_and_\nnewline"
SIMPLE_SECRET=secret123
EMPTY_VAL=""
SPACED_KEY = value_with_spaces_around_equal
WITH_HASH="value with # hash symbol"
DOLLAR_VAL="val_with_\$dollar"
`
	parsed, err := ParseEnvContent(envText)
	if err != nil {
		t.Fatalf("unexpected error parsing env content: %v", err)
	}

	expected := map[string]string{
		"APP_NAME":        "My Application",
		"PORT":            "8080",
		"DATABASE_URL":    "postgres://user:pass@localhost:5432/db",
		"SECRET":          "key_with_\"quotes\"_and_\nnewline",
		"SIMPLE_SECRET":   "secret123",
		"EMPTY_VAL":       "",
		"SPACED_KEY":      "value_with_spaces_around_equal",
		"WITH_HASH":       "value with # hash symbol",
		"DOLLAR_VAL":      "val_with_$dollar",
	}

	for k, expVal := range expected {
		if gotVal, ok := parsed[k]; !ok || gotVal != expVal {
			t.Errorf("key %q: expected %q, got %q (exists: %v)", k, expVal, gotVal, ok)
		}
	}

	// Test malformed line
	badText := "INVALID_LINE_WITHOUT_EQUALS"
	_, err = ParseEnvContent(badText)
	if err == nil {
		t.Error("expected error for line without '=', got nil")
	}

	// Test empty key line
	emptyKeyText := "=some_value"
	_, err = ParseEnvContent(emptyKeyText)
	if err == nil {
		t.Error("expected error for empty key, got nil")
	}

	// Test ParseEnvFile with non-existent file
	_, err = ParseEnvFile("/tmp/does_not_exist_12345.env")
	if err == nil {
		t.Error("expected error reading non-existent file, got nil")
	}
}

func TestEnvValidation(t *testing.T) {
	schemaContent := `
version: "1"
variables:
  - name: PORT
    type: integer
    required: true
  - name: SECRET_KEY
    type: secret
    required: true
  - name: SERVICE_URL
    type: url
    required: true
  - name: DEBUG_MODE
    type: boolean
    required: false
  - name: OPTIONAL_INT
    type: integer
    required: false
`
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, ".env.schema.yaml")
	if err := os.WriteFile(schemaPath, []byte(schemaContent), 0644); err != nil {
		t.Fatal(err)
	}

	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}

	// Case 1: Valid .env file
	validEnv := `
PORT=10050
SECRET_KEY=supersecretkey
SERVICE_URL=https://api.manova.space:8080/v1
DEBUG_MODE=true
OPTIONAL_INT=42
`
	validEnvPath := filepath.Join(tmpDir, ".env.valid")
	_ = os.WriteFile(validEnvPath, []byte(validEnv), 0600)

	errors := Validate(validEnvPath, schema)
	if len(errors) != 0 {
		t.Errorf("expected 0 validation errors for valid env, got %d: %+v", len(errors), errors)
	}

	// Test ValidateEnv alias
	errorsAlias := ValidateEnv(validEnvPath, schema)
	if len(errorsAlias) != 0 {
		t.Errorf("expected 0 validation errors with ValidateEnv alias, got %d", len(errorsAlias))
	}

	// Case 2: Invalid values (invalid int, invalid url, invalid bool, missing secret)
	invalidEnv := `
PORT=not_an_int
SERVICE_URL=not-a-valid-url
DEBUG_MODE=not_a_bool
OPTIONAL_INT=bad_int
`
	invalidEnvPath := filepath.Join(tmpDir, ".env.invalid")
	_ = os.WriteFile(invalidEnvPath, []byte(invalidEnv), 0600)

	errors = Validate(invalidEnvPath, schema)
	// Expected errors:
	// 1. PORT is invalid int
	// 2. SECRET_KEY is missing (required)
	// 3. SERVICE_URL is invalid url
	// 4. DEBUG_MODE is invalid boolean
	// 5. OPTIONAL_INT is invalid int
	if len(errors) != 5 {
		t.Errorf("expected 5 validation errors, got %d: %+v", len(errors), errors)
	}

	// Verify error types and Error() output
	errorTypes := make(map[string]string)
	for _, e := range errors {
		errorTypes[e.Variable] = e.Type
		if !strings.Contains(e.Error(), e.Variable) {
			t.Errorf("expected ValidationError.Error() to contain variable name %s, got %s", e.Variable, e.Error())
		}
	}

	if errorTypes["PORT"] != "invalid_integer" {
		t.Errorf("expected invalid_integer for PORT, got %s", errorTypes["PORT"])
	}
	if errorTypes["SECRET_KEY"] != "missing_required" {
		t.Errorf("expected missing_required for SECRET_KEY, got %s", errorTypes["SECRET_KEY"])
	}
	if errorTypes["SERVICE_URL"] != "invalid_url" {
		t.Errorf("expected invalid_url for SERVICE_URL, got %s", errorTypes["SERVICE_URL"])
	}
	if errorTypes["DEBUG_MODE"] != "invalid_boolean" {
		t.Errorf("expected invalid_boolean for DEBUG_MODE, got %s", errorTypes["DEBUG_MODE"])
	}
	if errorTypes["OPTIONAL_INT"] != "invalid_integer" {
		t.Errorf("expected invalid_integer for OPTIONAL_INT, got %s", errorTypes["OPTIONAL_INT"])
	}

	// Case 3: Empty string for required field
	emptyReqEnv := `
PORT=10050
SECRET_KEY=""
SERVICE_URL=https://api.manova.space
`
	emptyReqPath := filepath.Join(tmpDir, ".env.empty_req")
	_ = os.WriteFile(emptyReqPath, []byte(emptyReqEnv), 0600)

	errors = Validate(emptyReqPath, schema)
	if len(errors) != 1 || errors[0].Variable != "SECRET_KEY" || errors[0].Type != "missing_required" {
		t.Errorf("expected 1 missing_required error for SECRET_KEY, got: %+v", errors)
	}

	// Case 4: Non-existent env file
	nonExistentPath := filepath.Join(tmpDir, ".env.nonexistent")
	errors = Validate(nonExistentPath, schema)
	// File not found + 3 missing required fields
	if len(errors) != 4 {
		t.Errorf("expected 4 errors for non-existent file, got %d: %+v", len(errors), errors)
	}

	// Case 5: Syntax error in env file
	syntaxErrPath := filepath.Join(tmpDir, ".env.syntax_err")
	_ = os.WriteFile(syntaxErrPath, []byte("NOT_A_VALID_KEY_VALUE_LINE\n"), 0600)
	errors = Validate(syntaxErrPath, schema)
	if len(errors) < 1 || errors[0].Type != "syntax_error" {
		t.Errorf("expected syntax_error type, got %+v", errors)
	}

	// Case 6: Nil schema
	if errs := Validate(validEnvPath, nil); errs != nil {
		t.Errorf("expected nil for nil schema, got %+v", errs)
	}
}

func TestBooleanValidationValues(t *testing.T) {
	schema := &EnvSchema{
		Version: "1",
		Variables: []VariableDef{
			{Name: "FLAG", Type: TypeBoolean, Required: true},
		},
	}

	validBools := []string{"true", "false", "1", "0", "True", "False", "TRUE", "FALSE", "yes", "no", "t", "f"}
	for _, val := range validBools {
		errs := ValidateValues(map[string]string{"FLAG": val}, schema)
		if len(errs) != 0 {
			t.Errorf("expected %q to be a valid boolean, got errors: %+v", val, errs)
		}
	}

	invalidBools := []string{"maybe", "2", "-1", "none", "yep", "nope"}
	for _, val := range invalidBools {
		errs := ValidateValues(map[string]string{"FLAG": val}, schema)
		if len(errs) != 1 || errs[0].Type != "invalid_boolean" {
			t.Errorf("expected %q to fail boolean validation, got errors: %+v", val, errs)
		}
	}
}

func TestGenerateEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "subdir", ".env")

	// Pre-create file with 0644 to test chmod to 0600
	_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
	_ = os.WriteFile(targetPath, []byte("OLD=val"), 0644)

	values := map[string]string{
		"PORT":        "10050",
		"APP_NAME":    "Manova Platform",
		"WITH_SPACES": "value with spaces and # special chars",
		"WITH_ESCAPE": "line1\nline2\ttab\"quoted\"",
		"EMPTY":       "",
	}

	comments := map[string]string{
		"PORT":     "Main HTTP listening port\nConfigured by port allocator",
		"APP_NAME": "Application display name",
	}

	err := GenerateEnvFile(targetPath, values, comments)
	if err != nil {
		t.Fatalf("unexpected error generating env file: %v", err)
	}

	// Check file permissions are 0600
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("failed to stat generated file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected file permissions 0600, got %o", perm)
	}

	// Read and verify contents
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read generated file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "# Main HTTP listening port") {
		t.Errorf("expected comment for PORT in output")
	}
	if !strings.Contains(contentStr, "PORT=10050") {
		t.Errorf("expected PORT=10050 in output")
	}
	if !strings.Contains(contentStr, "APP_NAME=\"Manova Platform\"") {
		t.Errorf("expected quoted APP_NAME in output, got: %s", contentStr)
	}

	// Verify it parses back correctly
	parsed, err := ParseEnvFile(targetPath)
	if err != nil {
		t.Fatalf("failed to parse back generated env file: %v", err)
	}

	if parsed["PORT"] != "10050" || parsed["APP_NAME"] != "Manova Platform" || parsed["WITH_SPACES"] != "value with spaces and # special chars" || parsed["EMPTY"] != "" {
		t.Errorf("parsed back values do not match: %+v", parsed)
	}
	if parsed["WITH_ESCAPE"] != "line1\nline2\ttab\"quoted\"" {
		t.Errorf("parsed back WITH_ESCAPE does not match: got %q", parsed["WITH_ESCAPE"])
	}
}

func TestGenerateMCPEnv(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, ".cursor", "mcp.env")

	// Pre-create with 0644 to test chmod
	_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
	_ = os.WriteFile(targetPath, []byte("OLD=1"), 0644)

	tokens := map[string]string{
		"FORGEJO_ACCESS_TOKEN": "fe_token_123456",
		"TELEGRAM_BOT_TOKEN":   "tg_bot_token_789",
		"POSTGRES_MCP_URL":     "postgres://user:pass@localhost:10000/db",
	}

	err := GenerateMCPEnv(targetPath, tokens)
	if err != nil {
		t.Fatalf("unexpected error generating MCP env: %v", err)
	}

	// Check permissions 0600
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("failed to stat generated MCP env file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected permissions 0600, got %o", perm)
	}

	// Read and verify
	parsed, err := ParseEnvFile(targetPath)
	if err != nil {
		t.Fatalf("failed to parse back MCP env file: %v", err)
	}

	for k, v := range tokens {
		if parsed[k] != v {
			t.Errorf("key %q: expected %q, got %q", k, v, parsed[k])
		}
	}
}

func TestGenerateFromSchema(t *testing.T) {
	schema := &EnvSchema{
		Version: "1",
		Variables: []VariableDef{
			{Name: "PORT", Type: TypeInteger, Default: "10050", Description: "Port number", Required: true},
			{Name: "SECRET", Type: TypeSecret, Description: "API Secret", Required: true},
			{Name: "MODE", Type: TypeString, Default: "dev", Description: "App mode"},
		},
	}

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, ".env")

	overrides := map[string]string{
		"SECRET": "my-secret-key",
		"MODE":   "production",
	}

	err := GenerateFromSchema(targetPath, schema, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := ParseEnvFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}

	if parsed["PORT"] != "10050" {
		t.Errorf("expected PORT=10050 (default), got %s", parsed["PORT"])
	}
	if parsed["SECRET"] != "my-secret-key" {
		t.Errorf("expected SECRET=my-secret-key (override), got %s", parsed["SECRET"])
	}
	if parsed["MODE"] != "production" {
		t.Errorf("expected MODE=production (override), got %s", parsed["MODE"])
	}

	// Test nil schema
	err = GenerateFromSchema(targetPath, nil, overrides)
	if err == nil {
		t.Error("expected error for nil schema, got nil")
	}
}
