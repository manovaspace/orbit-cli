package env

import (
	"strings"
	"testing"
)

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
