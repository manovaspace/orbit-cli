package render_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
)

func TestEmitJSONSingleLineObject(t *testing.T) {
	var buf bytes.Buffer
	err := render.Emit(&buf, true, map[string]string{"scope": "orbit"}, func(*render.Printer) error {
		t.Fatal("text callback ran in JSON mode")
		return nil
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output does not end in a newline: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("output is not a single line: %q", out)
	}

	var envelope output.Envelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if !envelope.OK {
		t.Error("ok = false, want true")
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want an object", envelope.Data)
	}
	if data["scope"] != "orbit" {
		t.Errorf("data.scope = %v, want orbit", data["scope"])
	}
}

func TestEmitTextUsesPrinter(t *testing.T) {
	var buf bytes.Buffer
	err := render.Emit(&buf, false, nil, func(p *render.Printer) error {
		p.Line(render.StatusOK, "fine")
		return nil
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(buf.String(), "fine") {
		t.Errorf("output = %q, want the callback text", buf.String())
	}
}

func TestEmitReturnsCallbackError(t *testing.T) {
	want := errors.New("render")
	var buf bytes.Buffer
	err := render.Emit(&buf, false, nil, func(*render.Printer) error { return want })
	if !errors.Is(err, want) {
		t.Errorf("Emit error = %v, want %v", err, want)
	}
}

func TestEmitReturnsPrinterError(t *testing.T) {
	want := errors.New("write")
	err := render.Emit(&failingWriter{errs: []error{want}}, false, nil, func(p *render.Printer) error {
		p.Blank()
		return nil
	})
	if !errors.Is(err, want) {
		t.Errorf("Emit error = %v, want the printer error", err)
	}
}

func TestNewListNilItems(t *testing.T) {
	b, err := json.Marshal(render.NewList[string](nil, 0, 0, 0))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"items":[]`) {
		t.Errorf("JSON = %s, want an empty items array", b)
	}
}

func TestNewListLimitZeroMeansAll(t *testing.T) {
	list := render.NewList([]string{"a", "b"}, 2, 0, 0)
	if list.Page != 1 {
		t.Errorf("Page = %d, want 1", list.Page)
	}
	if list.Limit != 2 {
		t.Errorf("Limit = %d, want the total", list.Limit)
	}
}

func TestNewListKeepsPaging(t *testing.T) {
	list := render.NewList([]string{"a"}, 10, 2, 1)
	if list.Page != 2 || list.Limit != 1 || list.Total != 10 {
		t.Errorf("List = %+v, want page 2, limit 1, total 10", list)
	}
}

func TestEmitFailureJSON(t *testing.T) {
	want := errors.New("boom")
	var buf bytes.Buffer
	err := render.EmitFailure(&buf, true, want)
	if !errors.Is(err, want) {
		t.Errorf("EmitFailure = %v, want the original error", err)
	}

	var envelope output.Envelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if envelope.OK {
		t.Error("ok = true, want false")
	}
	if envelope.Error == nil || envelope.Error.Message != "boom" {
		t.Errorf("error = %+v, want the message", envelope.Error)
	}
}

func TestEmitFailureTextReturnsError(t *testing.T) {
	want := errors.New("boom")
	var buf bytes.Buffer
	if err := render.EmitFailure(&buf, false, want); !errors.Is(err, want) {
		t.Errorf("EmitFailure = %v, want the original error", err)
	}
	if buf.Len() != 0 {
		t.Errorf("text mode wrote %q, want nothing", buf.String())
	}
}

func TestEmitFindingsJSONKeepsDataAndError(t *testing.T) {
	failure := errors.New("3 checks failed")
	data := map[string]any{"findings": []string{"a", "b"}}

	var buf bytes.Buffer
	err := render.EmitFindings(&buf, true, data, failure, func(*render.Printer) error {
		t.Fatal("text callback ran in JSON mode")
		return nil
	})
	if !errors.Is(err, failure) {
		t.Errorf("EmitFindings = %v, want the failure", err)
	}

	var envelope output.Envelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if envelope.OK {
		t.Error("ok = true, want false")
	}
	if envelope.Error == nil || envelope.Error.Message != "3 checks failed" {
		t.Errorf("error = %+v, want the failure body", envelope.Error)
	}
	if _, ok := envelope.Data.(map[string]any); !ok {
		t.Fatalf("data = %T, want the findings object", envelope.Data)
	}
}

func TestEmitFindingsTextPrintsThenReturnsFailure(t *testing.T) {
	failure := errors.New("3 checks failed")
	var buf bytes.Buffer
	err := render.EmitFindings(&buf, false, nil, failure, func(p *render.Printer) error {
		p.Line(render.StatusFail, "a")
		return nil
	})
	if !errors.Is(err, failure) {
		t.Errorf("EmitFindings = %v, want the failure", err)
	}
	if !strings.Contains(buf.String(), "a") {
		t.Errorf("output = %q, want the callback text", buf.String())
	}
}
