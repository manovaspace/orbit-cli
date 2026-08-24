package doctor

import (
	"bytes"
	"context"
	"testing"
)

func TestDetectPackageManager(t *testing.T) {
	pm := DetectPackageManager()
	if pm == "" {
		t.Fatal("expected non-empty package manager detection")
	}
}

func TestAutoInstallDependencies_NilReport(t *testing.T) {
	ctx := context.Background()
	buf := new(bytes.Buffer)
	err := AutoInstallDependencies(ctx, nil, buf)
	if err != nil {
		t.Fatalf("unexpected error for nil report: %v", err)
	}
}

func TestAutoInstallDependencies_AllOK(t *testing.T) {
	ctx := context.Background()
	buf := new(bytes.Buffer)
	report := &DoctorReport{
		Results: []DiagnosticResult{
			{Name: "Go Compiler", Status: StatusOK},
			{Name: "Node.js", Status: StatusOK},
			{Name: "Bun", Status: StatusOK},
		},
	}
	err := AutoInstallDependencies(ctx, report, buf)
	if err != nil {
		t.Fatalf("unexpected error for OK report: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no install output for clean report, got: %s", buf.String())
	}
}
