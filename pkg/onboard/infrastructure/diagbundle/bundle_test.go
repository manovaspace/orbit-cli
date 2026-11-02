package diagbundle_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/diagbundle"
)

func testWriter() *diagbundle.Writer {
	return &diagbundle.Writer{Diagnostics: func() *doctor.DoctorReport {
		return &doctor.DoctorReport{Results: []doctor.DiagnosticResult{
			{Category: doctor.CategoryCore, Name: "Git", Status: doctor.StatusOK, Message: "git is installed"},
		}}
	}}
}

func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	entries := map[string][]byte{}
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read entry %s: %v", hdr.Name, err)
		}
		entries[hdr.Name] = content
	}
	return entries
}

func TestWriteSanitizesSessionAndMCPEnv(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".cursor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, ".cursor", "mcp.env"),
		[]byte("FORGEJO_TOKEN=fjo_tok_super_secret_123456\nPORT=10000\n"),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	session := &domain.SessionState{
		InviteToken:     "orbit-inv.eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.sensitive-signature",
		ForgejoToken:    "fjo_tok_super_secret_forgejo_token",
		WireGuardConfig: "[Interface]\nPrivateKey = sensitive_private_key_base64\nAddress = 10.8.0.5/24\n",
	}

	bundlePath := filepath.Join(t.TempDir(), "manova-diag-test.tar.gz")
	got, err := testWriter().Write(workspace, session, bundlePath)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if got != bundlePath {
		t.Errorf("path = %q, want %q", got, bundlePath)
	}

	entries := readArchive(t, bundlePath)
	for _, name := range []string{
		"doctor_report.json",
		"session_sanitized.json",
		"system_info.json",
		"mcp_env_sanitized.txt",
		"summary.txt",
	} {
		if _, ok := entries[name]; !ok {
			t.Errorf("missing archive entry %s", name)
		}
	}

	sanitizedSession := string(entries["session_sanitized.json"])
	if strings.Contains(sanitizedSession, "fjo_tok_super_secret_forgejo_token") {
		t.Errorf("git token was not sanitized: %s", sanitizedSession)
	}
	if !strings.Contains(sanitizedSession, "fjo_tok_***") {
		t.Errorf("expected the masked git token: %s", sanitizedSession)
	}
	if strings.Contains(sanitizedSession, "sensitive_private_key_base64") {
		t.Errorf("VPN private key was not sanitized: %s", sanitizedSession)
	}

	sanitizedEnv := string(entries["mcp_env_sanitized.txt"])
	if strings.Contains(sanitizedEnv, "fjo_tok_super_secret_123456") {
		t.Errorf("MCP token was not sanitized: %s", sanitizedEnv)
	}
	if !strings.Contains(sanitizedEnv, "FORGEJO_TOKEN=[REDACTED]") {
		t.Errorf("expected a redacted token: %s", sanitizedEnv)
	}
	if !strings.Contains(sanitizedEnv, "PORT=10000") {
		t.Errorf("non-sensitive values must survive: %s", sanitizedEnv)
	}
}

func TestWriteWithoutSessionRecordsNoPendingSession(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if _, err := testWriter().Write(t.TempDir(), nil, bundlePath); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	entries := readArchive(t, bundlePath)
	payload := string(entries["session_sanitized.json"])
	if !strings.Contains(payload, "no_pending_session") {
		t.Errorf("expected the no-session marker, got %s", payload)
	}
}

func TestWriteDefaultNameLandsInWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	path, err := testWriter().Write(t.TempDir(), nil, "default")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(path), "orbit-diag-") || !strings.HasSuffix(path, ".tar.gz") {
		t.Errorf("unexpected default bundle name %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("bundle not created: %v", err)
	}
}
