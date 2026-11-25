package domain

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestParseSchemaFromAndFor checks that the from and for keys of a variable survive YAML
// unmarshaling; before they were added to rawVar they were silently dropped.
func TestParseSchemaFromAndFor(t *testing.T) {
	content := `
version: "1"
variables:
  - name: DATABASE_URL
    type: url
    from: postgres:app_api
    required: true
  - name: BUILD_TOKEN
    type: secret
    from: secret:build
    for: [build, prod]
`
	schema, err := ParseSchema([]byte(content))
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	db := schema.GetVariable("DATABASE_URL")
	if db == nil || db.From != "postgres:app_api" || len(db.For) != 0 {
		t.Fatalf("DATABASE_URL From/For = %+v", db)
	}
	tok := schema.GetVariable("BUILD_TOKEN")
	if tok == nil || tok.From != "secret:build" || len(tok.For) != 2 || tok.For[0] != "build" || tok.For[1] != "prod" {
		t.Fatalf("BUILD_TOKEN From/For = %+v", tok)
	}
}

// TestVariableDefFromAndForYAMLKeys checks the yaml keys and omitempty on the struct itself.
func TestVariableDefFromAndForYAMLKeys(t *testing.T) {
	out, err := yaml.Marshal(VariableDef{Name: "X", Type: TypeString, From: "mail", For: []string{"dev"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(out)
	for _, want := range []string{"from: mail", "for:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled YAML %q lacks %q", got, want)
		}
	}
}
