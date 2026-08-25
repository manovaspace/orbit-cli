package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/onboard"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

func testSigningSecret() []byte {
	return []byte("test-super-secret-signing-key-32bytes-long!")
}

func TestOnboardFlagsAndResumePrompt(t *testing.T) {
	cmd := newOnboardCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--dry-run"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("onboard command dry-run failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "MANOVA DEVELOPER WIZARD") {
		t.Errorf("output missing onboarding banner: %s", out)
	}
	if !strings.Contains(out, "DRY-RUN PREVIEW") {
		t.Errorf("output missing dry run preview card: %s", out)
	}
}

func TestOnboardNonInteractiveFullProgression(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")
	sshDir := filepath.Join(tempDir, "ssh")
	_ = os.MkdirAll(workspaceDir, 0755)
	_ = os.MkdirAll(sshDir, 0700)

	// 1. Setup mock onboard edge HTTP server
	secret := testSigningSecret()
	prov := provisioner.NewDevProvisioner()
	srvConfig := onboard.ServerConfig{
		Secret:           secret,
		Provisioner:      prov,
		DisableRateLimit: true,
	}
	srvHandler, err := onboard.NewServer(srvConfig)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	server := httptest.NewServer(srvHandler.Handler())
	defer server.Close()

	// 2. Generate valid invite token
	req := invite.InviteRequest{
		Email:       "test-dev@manova.space",
		DisplayName: "Test Developer",
		Scope:       "core",
		TTL:         24 * time.Hour,
	}
	tokenStr, _, err := invite.GenerateToken(req, secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// 2.5 Pre-seed session stage to StageDoctorPassed to simulate passing diagnostics in CI/test environment
	smPre, err := session.NewSessionManager(sessionPath)
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	_ = smPre.SaveSession(&session.Session{
		ID:           "test-sess-preflight",
		CurrentStage: session.StageDoctorPassed,
	})

	// 3. Execute onboard in non-interactive mode
	buf := new(bytes.Buffer)
	cmd := newOnboardCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--non-interactive",
		"--resume",
		"--token", tokenStr,
		"--edge-url", server.URL,
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
		"--ssh-dir", sshDir,
		"--skip-stack",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("onboard command execution failed: %v\nOutput:\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "ONBOARDING COMPLETE") {
		t.Errorf("output missing completion card: %s", out)
	}
	if !strings.Contains(out, "test-dev") {
		t.Errorf("output missing provisioned UID: %s", out)
	}

	// 4. Verify session is saved and completed
	sm, err := session.NewSessionManager(sessionPath)
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	s, err := sm.LoadSession()
	if err != nil || s == nil {
		t.Fatalf("failed to load saved session: %v", err)
	}
	if s.CurrentStage != session.StageCompleted {
		t.Errorf("expected stage %s, got %s", session.StageCompleted, s.CurrentStage)
	}
	if s.UID != "test-dev" {
		t.Errorf("expected UID test-dev, got %s", s.UID)
	}
	if s.ForgejoToken == "" {
		t.Error("expected ForgejoToken to be saved in session")
	}

	// 5. Verify .cursor/mcp.env was configured
	mcpEnvFile := filepath.Join(workspaceDir, ".cursor", "mcp.env")
	envData, err := os.ReadFile(mcpEnvFile)
	if err != nil {
		t.Fatalf("expected .cursor/mcp.env to exist: %v", err)
	}
	if !strings.Contains(string(envData), s.ForgejoToken) {
		t.Errorf("expected .cursor/mcp.env to contain forgejo token, got:\n%s", string(envData))
	}
}

func TestOnboardSessionCheckpointAndResume(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")
	sshDir := filepath.Join(tempDir, "ssh")
	_ = os.MkdirAll(workspaceDir, 0755)
	_ = os.MkdirAll(sshDir, 0700)

	// 1. Setup HTTP edge server
	secret := testSigningSecret()
	prov := provisioner.NewDevProvisioner()
	srvConfig := onboard.ServerConfig{
		Secret:           secret,
		Provisioner:      prov,
		DisableRateLimit: true,
	}
	srvHandler, _ := onboard.NewServer(srvConfig)
	server := httptest.NewServer(srvHandler.Handler())
	defer server.Close()

	// 2. Pre-populate a session at StageKeypairReady
	sm, _ := session.NewSessionManager(sessionPath)
	s := sm.CreateSession("resumed-dev@manova.space", "Resumed Dev")
	s.CurrentStage = session.StageKeypairReady
	s.SSHPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICheckpointTestPubKey resumed-dev@manova.space"
	if err := sm.SaveSession(s); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// 3. Generate invite token
	req := invite.InviteRequest{
		Email:       "resumed-dev@manova.space",
		DisplayName: "Resumed Dev",
		Scope:       "core",
		TTL:         24 * time.Hour,
	}
	tokenStr, _, _ := invite.GenerateToken(req, secret)

	// 4. Run with --resume flag
	buf := new(bytes.Buffer)
	cmd := newOnboardCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--resume",
		"--token", tokenStr,
		"--edge-url", server.URL,
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
		"--ssh-dir", sshDir,
		"--non-interactive",
		"--skip-stack",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("onboard --resume failed: %v\nOutput:\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Resuming existing session") && !strings.Contains(out, "cached checkpoint") {
		t.Errorf("output missing resume indication: %s", out)
	}

	// 5. Verify session advanced to completed
	loaded, err := sm.LoadSession()
	if err != nil || loaded == nil {
		t.Fatalf("failed to reload session: %v", err)
	}
	if loaded.CurrentStage != session.StageCompleted {
		t.Errorf("expected stage %s, got %s", session.StageCompleted, loaded.CurrentStage)
	}
	if loaded.UID != "resumed-dev" {
		t.Errorf("expected UID resumed-dev, got %s", loaded.UID)
	}
}

func TestOnboardIgnoreAndRemoveCheckpointFlag(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")

	// Pre-create pending session
	sm, _ := session.NewSessionManager(sessionPath)
	s := sm.CreateSession("reset-me@manova.space", "Reset Me")
	s.CurrentStage = session.StageTokenClaimed
	_ = sm.SaveSession(s)

	if !sm.HasPendingSession() {
		t.Fatal("expected pending session before reset")
	}

	buf := new(bytes.Buffer)
	cmd := newOnboardCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--ignore-and-remove-checkpoint",
		"--session-file", sessionPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("onboard --ignore-and-remove-checkpoint failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "session cleared") {
		t.Errorf("output missing reset confirmation: %s", out)
	}

	// Check file is removed
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Errorf("expected session file to be removed after reset")
	}
}

func TestOnboardRollbackFlag(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")

	repo1 := filepath.Join(workspaceDir, "orbit", "orbit-toolkit")
	repo2 := filepath.Join(workspaceDir, "clients", "client-a")
	_ = os.MkdirAll(repo1, 0755)
	_ = os.MkdirAll(repo2, 0755)
	_ = os.WriteFile(filepath.Join(repo1, "README.md"), []byte("test"), 0644)

	cursorEnv := filepath.Join(workspaceDir, ".cursor", "mcp.env")
	_ = os.MkdirAll(filepath.Dir(cursorEnv), 0755)
	_ = os.WriteFile(cursorEnv, []byte("FORGEJO_TOKEN=test_tok"), 0600)

	// Pre-populate session with cloned repos
	sm, _ := session.NewSessionManager(sessionPath)
	s := sm.CreateSession("rollback-user@manova.space", "Rollback User")
	s.UID = "rollback-user"
	s.CurrentStage = session.StageReposCloned
	s.ClonedRepos = []string{
		filepath.Join("orbit", "orbit-toolkit"),
		filepath.Join("clients", "client-a"),
	}
	_ = sm.SaveSession(s)

	buf := new(bytes.Buffer)
	cmd := newOnboardCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--rollback",
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("onboard --rollback failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Rollback completed successfully") {
		t.Errorf("output missing rollback confirmation: %s", out)
	}

	// Verify cloned repos were removed
	if _, err := os.Stat(repo1); !os.IsNotExist(err) {
		t.Errorf("expected repo1 %s to be deleted by rollback", repo1)
	}
	if _, err := os.Stat(repo2); !os.IsNotExist(err) {
		t.Errorf("expected repo2 %s to be deleted by rollback", repo2)
	}

	// Verify session file is removed
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Errorf("expected session file to be removed after rollback")
	}
}

func TestOnboardDiagBundle(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")
	bundlePath := filepath.Join(tempDir, "manova-diag-test.tar.gz")

	_ = os.MkdirAll(filepath.Join(workspaceDir, ".cursor"), 0755)
	_ = os.WriteFile(
		filepath.Join(workspaceDir, ".cursor", "mcp.env"),
		[]byte("FORGEJO_TOKEN=fjo_tok_super_secret_123456\nPORT=10000\n"),
		0600,
	)

	// Save session with secret data
	sm, _ := session.NewSessionManager(sessionPath)
	s := sm.CreateSession("diag-test@manova.space", "Diag Tester")
	s.InviteToken = "manova-inv.eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.sensitive-signature"
	s.ForgejoToken = "fjo_tok_super_secret_forgejo_token"
	s.WireGuardConfig = "[Interface]\nPrivateKey = sensitive_private_key_base64\nAddress = 10.8.0.5/24\n"
	_ = sm.SaveSession(s)

	buf := new(bytes.Buffer)
	cmd := newOnboardCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--diag-bundle", bundlePath,
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("onboard --diag-bundle failed: %v\nOutput: %s", err, buf.String())
	}

	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("diagnostic bundle file was not created at %s: %v", bundlePath, err)
	}

	// Open and inspect tar.gz archive
	f, err := os.Open(bundlePath)
	if err != nil {
		t.Fatalf("failed to open bundle: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	foundEntries := make(map[string][]byte)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("failed to read tar entry %s: %v", hdr.Name, err)
		}
		foundEntries[hdr.Name] = content
	}

	// Verify expected files exist in archive
	expectedFiles := []string{
		"doctor_report.json",
		"session_sanitized.json",
		"system_info.json",
		"mcp_env_sanitized.txt",
		"summary.txt",
	}

	for _, ef := range expectedFiles {
		if _, ok := foundEntries[ef]; !ok {
			t.Errorf("missing expected tar entry %s", ef)
		}
	}

	// Verify sanitization of session
	sanitizedSessionStr := string(foundEntries["session_sanitized.json"])
	if strings.Contains(sanitizedSessionStr, "fjo_tok_super_secret_forgejo_token") {
		t.Errorf("sensitive ForgejoToken was not sanitized in session: %s", sanitizedSessionStr)
	}
	if !strings.Contains(sanitizedSessionStr, "fjo_tok_***") {
		t.Errorf("expected masked token in sanitized session: %s", sanitizedSessionStr)
	}
	if strings.Contains(sanitizedSessionStr, "sensitive_private_key_base64") {
		t.Errorf("sensitive WireGuard private key was not sanitized: %s", sanitizedSessionStr)
	}

	// Verify sanitization of mcp.env
	sanitizedEnvStr := string(foundEntries["mcp_env_sanitized.txt"])
	if strings.Contains(sanitizedEnvStr, "fjo_tok_super_secret_123456") {
		t.Errorf("sensitive MCP token was not sanitized in mcp.env: %s", sanitizedEnvStr)
	}
	if !strings.Contains(sanitizedEnvStr, "FORGEJO_TOKEN=[REDACTED]") {
		t.Errorf("expected FORGEJO_TOKEN=[REDACTED] in sanitized mcp.env: %s", sanitizedEnvStr)
	}
	if !strings.Contains(sanitizedEnvStr, "PORT=10000") {
		t.Errorf("expected non-sensitive variable PORT=10000 to be preserved: %s", sanitizedEnvStr)
	}
}

func TestOnboardJsonProgressStream(t *testing.T) {
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")

	buf := new(bytes.Buffer)
	cmd := newOnboardCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--json", "--dry-run", "--session-file", sessionPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("onboard --json --dry-run failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("expected at least one JSON line emitted")
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var evt OnboardProgressEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("failed to parse JSON event %q: %v", line, err)
		}
		if evt.Stage == "" || evt.Status == "" {
			t.Errorf("event missing required fields: %+v", evt)
		}
	}
}
