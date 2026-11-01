package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

func TestSuccessEnvelope(t *testing.T) {
	env := Success(map[string]string{"scope": "orbit"})
	if env.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", env.SchemaVersion)
	}
	if !env.OK {
		t.Error("OK = false, want true")
	}
	if env.Error != nil {
		t.Errorf("Error = %v, want nil", env.Error)
	}
	if env.Data == nil {
		t.Error("Data = nil, want the supplied value")
	}
}

func TestFailureFromClierr(t *testing.T) {
	err := clierr.WithHint(clierr.Auth("token rejected"), "run orbit staff login")
	env := Failure(err)

	if env.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", env.SchemaVersion)
	}
	if env.OK {
		t.Error("OK = true, want false")
	}
	if env.Data != nil {
		t.Errorf("Data = %v, want nil", env.Data)
	}
	if env.Error == nil {
		t.Fatal("Error = nil, want a body")
	}
	if env.Error.Code != "auth" {
		t.Errorf("Code = %q, want %q", env.Error.Code, "auth")
	}
	if env.Error.Message != "token rejected" {
		t.Errorf("Message = %q, want %q", env.Error.Message, "token rejected")
	}
	if env.Error.Hint != "run orbit staff login" {
		t.Errorf("Hint = %q, want %q", env.Error.Hint, "run orbit staff login")
	}
}

func TestFailureFromPlainError(t *testing.T) {
	env := Failure(errors.New("boom"))
	if env.Error == nil {
		t.Fatal("Error = nil, want a body")
	}
	if env.Error.Code != "failed" {
		t.Errorf("Code = %q, want %q", env.Error.Code, "failed")
	}
	if env.Error.Message != "boom" {
		t.Errorf("Message = %q, want %q", env.Error.Message, "boom")
	}
	if env.Error.Hint != "" {
		t.Errorf("Hint = %q, want empty", env.Error.Hint)
	}
}

func TestWriteSingleLine(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Success(map[string]string{"scope": "orbit"})); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output does not end in a newline: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("output is not a single line: %q", out)
	}

	var decoded Envelope
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", decoded.SchemaVersion)
	}
	if !strings.HasPrefix(out, `{"schemaVersion":1,"ok":true,"data":`) {
		t.Errorf("unexpected key order: %q", out)
	}
}

func TestWriteOmitsNilData(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Success(nil)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if strings.Contains(buf.String(), `"data"`) {
		t.Errorf("data key present for nil payload: %q", buf.String())
	}
}

func TestWriteFailureShape(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Failure(clierr.Network("unreachable"))); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`"ok":false`, `"error":{"code":"network","message":"unreachable"}`} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
	if strings.Contains(out, `"data"`) {
		t.Errorf("data key present on failure: %q", out)
	}
}
