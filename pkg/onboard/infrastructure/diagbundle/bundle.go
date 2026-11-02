// Package diagbundle writes the sanitized onboarding diagnostic archive.
package diagbundle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
)

// Writer builds diagnostic bundles.
type Writer struct {
	// Diagnostics runs the doctor report; nil means doctor.RunDiagnostics.
	Diagnostics func() *doctor.DoctorReport
}

// New builds a Writer using the default doctor run.
func New() *Writer {
	return &Writer{}
}

// Write creates the archive and returns its path. The session may be nil, in
// which case the bundle records that no session is pending.
func (w *Writer) Write(workspaceRoot string, session *domain.SessionState, customOutPath string) (string, error) {
	var outPath string
	if customOutPath != "" && customOutPath != "default" {
		outPath = customOutPath
	} else {
		timestamp := time.Now().UTC().Format("20060102-150405")
		outPath = fmt.Sprintf("orbit-diag-%s.tar.gz", timestamp)
	}

	outDir := filepath.Dir(outPath)
	if outDir != "." && outDir != "" {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory for bundle: %w", err)
		}
	}

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("failed to create diagnostic bundle file %s: %w", outPath, err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	// 1. Doctor report
	report := w.diagnostics()()
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if err := addTarFile(tw, "doctor_report.json", reportJSON); err != nil {
		return "", err
	}

	// 2. Sanitized session
	var sessData []byte
	if sanitized := domain.SanitizeSession(session); sanitized != nil {
		sessData, _ = json.MarshalIndent(sanitized, "", "  ")
	}
	if len(sessData) == 0 {
		sessData = []byte("{\n  \"status\": \"no_pending_session\"\n}\n")
	}
	if err := addTarFile(tw, "session_sanitized.json", sessData); err != nil {
		return "", err
	}

	// 3. System info
	hostname, _ := os.Hostname()
	sysInfo := map[string]interface{}{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"go_version": runtime.Version(),
		"num_cpu":    runtime.NumCPU(),
		"hostname":   hostname,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	sysInfoJSON, _ := json.MarshalIndent(sysInfo, "", "  ")
	if err := addTarFile(tw, "system_info.json", sysInfoJSON); err != nil {
		return "", err
	}

	// 4. Sanitized .cursor/mcp.env
	mcpEnvPath := filepath.Join(workspaceRoot, ".cursor", "mcp.env")
	if envData, err := os.ReadFile(mcpEnvPath); err == nil {
		sanitizedEnv := domain.SanitizeEnvContent(string(envData))
		if err := addTarFile(tw, "mcp_env_sanitized.txt", []byte(sanitizedEnv)); err != nil {
			return "", err
		}
	}

	// 5. Summary
	var summary strings.Builder
	summary.WriteString("=== MANOVA ONBOARDING DIAGNOSTIC BUNDLE ===\n")
	fmt.Fprintf(&summary, "Generated: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&summary, "OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&summary, "Go Version: %s\n", runtime.Version())
	fmt.Fprintf(&summary, "Doctor Checks: %d, Errors: %v, Warnings: %v\n", len(report.Results), report.HasErrors(), report.HasWarnings())
	if err := addTarFile(tw, "summary.txt", []byte(summary.String())); err != nil {
		return "", err
	}

	return outPath, nil
}

func (w *Writer) diagnostics() func() *doctor.DoctorReport {
	if w.Diagnostics != nil {
		return w.Diagnostics
	}
	return doctor.RunDiagnostics
}

func addTarFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0600,
		Size:    int64(len(data)),
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("failed to write tar body for %s: %w", name, err)
	}
	return nil
}
