package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvCheckAndSetup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("MANOVA_ROOT", tmpDir)

	subDir := filepath.Join(tmpDir, "service-a")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	schemaPath := filepath.Join(subDir, ".env.schema.yaml")
	schemaContent := `
version: "1"
variables:
  - name: SERVICE_PORT
    type: integer
    default: 10050
    required: true
  - name: APP_NAME
    type: string
    default: "service-a"
`
	if err := os.WriteFile(schemaPath, []byte(schemaContent), 0644); err != nil {
		t.Fatalf("failed to write schema: %v", err)
	}

	// 1. Test env setup
	setupBuf := new(bytes.Buffer)
	setupCmd := newRootCmd()
	setupCmd.SetOut(setupBuf)
	setupCmd.SetErr(setupBuf)
	setupCmd.SetArgs([]string{"env", "setup", subDir})

	if err := setupCmd.Execute(); err != nil {
		t.Fatalf("env setup failed: %v", err)
	}

	generatedEnv := filepath.Join(subDir, ".env")
	if _, err := os.Stat(generatedEnv); err != nil {
		t.Fatalf("expected .env file to be generated: %v", err)
	}

	// 2. Test env check on the generated valid file
	checkBuf := new(bytes.Buffer)
	checkCmd := newRootCmd()
	checkCmd.SetOut(checkBuf)
	checkCmd.SetErr(checkBuf)
	checkCmd.SetArgs([]string{"env", "check", subDir})

	if err := checkCmd.Execute(); err != nil {
		t.Fatalf("env check failed on valid generated env: %v", err)
	}

	output := checkBuf.String()
	if !strings.Contains(output, ".env valid") {
		t.Errorf("expected '.env valid' in output, got: %s", output)
	}
}
