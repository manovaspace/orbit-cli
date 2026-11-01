package domain

import (
	"io/fs"
	"strings"
	"testing"
)

func TestParseSchemaValid(t *testing.T) {
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
	schema, err := ParseSchema([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error loading schema: %v", err)
	}
	if schema.Version != "1" {
		t.Errorf("expected version '1', got %s", schema.Version)
	}
	if len(schema.Variables) != 4 {
		t.Fatalf("expected 4 variables, got %d", len(schema.Variables))
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
	if boolVar == nil || boolVar.Default != "true" {
		t.Fatalf("boolean default not decoded: %+v", boolVar)
	}

	if len(schema.RequiredVariables()) != 2 {
		t.Errorf("expected 2 required variables, got %d", len(schema.RequiredVariables()))
	}
	if schema.GetVariable("NON_EXISTENT") != nil {
		t.Error("unexpected non-existent variable")
	}
}

func TestParseSchemaInvalid(t *testing.T) {
	if _, err := ParseSchema([]byte("   \n")); err == nil {
		t.Error("expected error for empty schema")
	}
	if _, err := ParseSchema([]byte("version: [unclosed\n")); err == nil {
		t.Error("expected error for malformed schema")
	}
	if _, err := ParseSchema([]byte("variables:\n  - name: FOO\n")); err == nil {
		t.Error("expected error for missing version")
	}
}

func TestParseEnvContent(t *testing.T) {
	envText := `
# System configuration
export APP_NAME="My Application"
PORT=8080 # default port
DATABASE_URL='postgres://user:pass@localhost:5432/db'

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
		"APP_NAME":      "My Application",
		"PORT":          "8080",
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/db",
		"SECRET":        "key_with_\"quotes\"_and_\nnewline",
		"SIMPLE_SECRET": "secret123",
		"EMPTY_VAL":     "",
		"SPACED_KEY":    "value_with_spaces_around_equal",
		"WITH_HASH":     "value with # hash symbol",
		"DOLLAR_VAL":    "val_with_$dollar",
	}
	for k, expVal := range expected {
		if gotVal, ok := parsed[k]; !ok || gotVal != expVal {
			t.Errorf("key %q: expected %q, got %q (exists: %v)", k, expVal, gotVal, ok)
		}
	}

	if _, err := ParseEnvContent("INVALID_LINE_WITHOUT_EQUALS"); err == nil {
		t.Error("expected error for line without '='")
	}
	if _, err := ParseEnvContent("=some_value"); err == nil {
		t.Error("expected error for empty key")
	}
}

func TestValidateValues(t *testing.T) {
	schema := &EnvSchema{
		Version: "1",
		Variables: []VariableDef{
			{Name: "PORT", Type: TypeInteger, Required: true},
			{Name: "SECRET_KEY", Type: TypeSecret, Required: true},
			{Name: "SERVICE_URL", Type: TypeURL, Required: true},
			{Name: "DEBUG_MODE", Type: TypeBoolean, Required: false},
			{Name: "OPTIONAL_INT", Type: TypeInteger, Required: false},
		},
	}

	valid := map[string]string{
		"PORT": "10050", "SECRET_KEY": "supersecretkey",
		"SERVICE_URL": "https://api.manova.space:8080/v1",
		"DEBUG_MODE":  "true", "OPTIONAL_INT": "42",
	}
	if errs := ValidateValues(valid, schema); len(errs) != 0 {
		t.Errorf("expected 0 validation errors, got %d: %+v", len(errs), errs)
	}

	invalid := map[string]string{
		"PORT": "not_an_int", "SERVICE_URL": "not-a-valid-url",
		"DEBUG_MODE": "not_a_bool", "OPTIONAL_INT": "bad_int",
	}
	errs := ValidateValues(invalid, schema)
	if len(errs) != 5 {
		t.Fatalf("expected 5 validation errors, got %d: %+v", len(errs), errs)
	}
	types := make(map[string]string)
	for _, e := range errs {
		types[e.Variable] = e.Type
		if !strings.Contains(e.Error(), e.Variable) {
			t.Errorf("ValidationError.Error() missing variable %s: %s", e.Variable, e.Error())
		}
	}
	if types["PORT"] != "invalid_integer" || types["SECRET_KEY"] != "missing_required" ||
		types["SERVICE_URL"] != "invalid_url" || types["DEBUG_MODE"] != "invalid_boolean" ||
		types["OPTIONAL_INT"] != "invalid_integer" {
		t.Errorf("unexpected error types: %+v", types)
	}

	if errs := ValidateValues(map[string]string{"SECRET_KEY": `""`}, schema); len(errs) == 0 {
		t.Error("empty required value accepted")
	}
}

func TestBooleanValidationValues(t *testing.T) {
	schema := &EnvSchema{Version: "1", Variables: []VariableDef{{Name: "FLAG", Type: TypeBoolean, Required: true}}}
	for _, val := range []string{"true", "false", "1", "0", "True", "False", "TRUE", "FALSE", "yes", "no", "t", "f"} {
		if errs := ValidateValues(map[string]string{"FLAG": val}, schema); len(errs) != 0 {
			t.Errorf("%q rejected: %+v", val, errs)
		}
	}
	for _, val := range []string{"maybe", "2", "-1", "none", "yep", "nope"} {
		if errs := ValidateValues(map[string]string{"FLAG": val}, schema); len(errs) != 1 || errs[0].Type != "invalid_boolean" {
			t.Errorf("%q accepted: %+v", val, errs)
		}
	}
}

func TestMissingEnvFileErrors(t *testing.T) {
	schema := &EnvSchema{Variables: []VariableDef{{Name: "A", Required: true}, {Name: "B"}}}
	errs := MissingEnvFileErrors("/tmp/x/.env", fs.ErrNotExist, schema)
	if len(errs) != 2 || errs[0].Type != "file_not_found" || errs[1].Type != "missing_required" {
		t.Fatalf("errors = %+v", errs)
	}
}

func TestRenderEnvAndMCP(t *testing.T) {
	content := RenderEnv(map[string]string{
		"PORT":        "10050",
		"APP_NAME":    "Manova Platform",
		"WITH_ESCAPE": "line1\nline2\ttab\"quoted\"",
		"EMPTY":       "",
	}, map[string]string{"PORT": "Main HTTP listening port\nConfigured by port allocator"})
	if !strings.Contains(content, "# Main HTTP listening port") || !strings.Contains(content, "# Configured by port allocator") {
		t.Errorf("comments missing: %s", content)
	}
	if !strings.Contains(content, "PORT=10050") || !strings.Contains(content, `APP_NAME="Manova Platform"`) || !strings.Contains(content, `EMPTY=""`) {
		t.Errorf("values missing: %s", content)
	}

	parsed, err := ParseEnvContent(content)
	if err != nil {
		t.Fatal(err)
	}
	if parsed["WITH_ESCAPE"] != "line1\nline2\ttab\"quoted\"" || parsed["EMPTY"] != "" {
		t.Errorf("round-trip mismatch: %+v", parsed)
	}

	mcp := RenderMCPEnv(map[string]string{"TELEGRAM_BOT_TOKEN": "token", "POSTGRES_MCP_URL": "postgres://u:p@localhost/db"})
	if !strings.Contains(mcp, "Auto-generated by manova CLI") || !strings.Contains(mcp, "TELEGRAM_BOT_TOKEN=token") {
		t.Errorf("mcp content = %s", mcp)
	}
}

func TestInvalidInputsNeverExposeValues(t *testing.T) {
	secret := "synthetic-credential-canary"
	for _, typ := range []VarType{TypeInteger, TypeURL, TypeBoolean} {
		schema := &EnvSchema{Variables: []VariableDef{{Name: "INPUT", Type: typ, Required: true}}}
		errors := ValidateValues(map[string]string{"INPUT": "https://user:" + secret + "@bad host"}, schema)
		if len(errors) == 0 {
			t.Fatal("expected invalid input")
		}
		for _, err := range errors {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s leaked input: %s", typ, err.Error())
			}
		}
	}
	_, err := ParseEnvContent(secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("syntax error exposes input: %v", err)
	}
	if errors := ValidateValues(nil, nil); len(errors) == 0 {
		t.Error("nil contract incorrectly validates")
	}
}
