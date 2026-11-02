package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	invitedomain "github.com/manovaspace/orbit-cli/pkg/invite/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/application"
	onboarddomain "github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/devprovisioner"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/hostfs"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/server"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/sessionstore"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/spf13/cobra"
)

func testSigningSecret() []byte {
	return []byte("test-super-secret-signing-key-32bytes-long!")
}

// fakeDiagnostics reports a clean machine and never heals anything.
type fakeDiagnostics struct {
	report *doctor.DoctorReport
}

func okDiagnostics() *fakeDiagnostics {
	return &fakeDiagnostics{report: &doctor.DoctorReport{Results: []doctor.DiagnosticResult{
		{Category: doctor.CategoryCore, Name: "Git", Status: doctor.StatusOK, Message: "git is installed"},
	}}}
}

func (f *fakeDiagnostics) Run() *doctor.DoctorReport { return f.report }

func (f *fakeDiagnostics) HealableCount([]doctor.DiagnosticResult) int { return 0 }

func (f *fakeDiagnostics) Heal(context.Context, []doctor.DiagnosticResult, func(string, string)) error {
	return nil
}

// fakeSSH returns a fixed public key.
type fakeSSH struct{}

func (fakeSSH) EnsureKey(string, string) (string, error) {
	return "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey test@manova.space", nil
}

// fakeBundle records the requested bundle path.
type fakeBundle struct {
	path string
}

func (f *fakeBundle) Write(string, *onboarddomain.SessionState, string) (string, error) {
	if f.path == "" {
		f.path = "orbit-diag-test.tar.gz"
	}
	return f.path, nil
}

// withOnboardAdapters replaces the service adapters for one command test.
func withOnboardAdapters(t *testing.T, mutate func(*application.Service)) {
	t.Helper()
	originalService := onboardService
	originalTTY := onboardIsTTY
	t.Cleanup(func() {
		onboardService = originalService
		onboardIsTTY = originalTTY
	})
	onboardService = func(cmd *cobra.Command, opts *onboardOptions, out io.Writer) (*application.Service, error) {
		svc, err := newOnboardService(cmd, opts, out)
		if err != nil {
			return nil, err
		}
		svc.Diagnostics = okDiagnostics()
		svc.SSH = fakeSSH{}
		svc.Stack = nil
		env := hostfs.New(nil)
		env.LookPath = func(string) (string, error) { return "", errors.New("not installed") }
		svc.Environment = env
		if mutate != nil {
			mutate(svc)
		}
		return svc, nil
	}
}

func runOnboardCmd(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cmd := newOnboardCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	if in != "" {
		cmd.SetIn(bytes.NewBufferString(in))
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestOnboardFlagsAndResumePrompt(t *testing.T) {
	withOnboardAdapters(t, nil)
	t.Setenv("HOME", t.TempDir())

	out, err := runOnboardCmd(t, "",
		"--dry-run",
		"--session-file", filepath.Join(t.TempDir(), "session.json"),
	)
	if err != nil {
		t.Fatalf("onboard --dry-run failed: %v", err)
	}
	if !strings.Contains(out, "ORBIT DEVELOPER WIZARD") {
		t.Errorf("output missing onboarding banner: %s", out)
	}
	if !strings.Contains(out, "DRY-RUN PREVIEW") {
		t.Errorf("output missing dry run preview card: %s", out)
	}
}

func TestOnboardJSONEnvelope(t *testing.T) {
	withOnboardAdapters(t, nil)
	t.Setenv("HOME", t.TempDir())

	out, err := runOnboardCmd(t, "",
		"--json",
		"--dry-run",
		"--session-file", filepath.Join(t.TempDir(), "session.json"),
	)
	if err != nil {
		t.Fatalf("onboard --json --dry-run failed: %v", err)
	}

	var env output.Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("output is not one JSON envelope: %v\n%s", err, out)
	}
	if env.SchemaVersion != output.SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", env.SchemaVersion, output.SchemaVersion)
	}
	if !env.OK {
		t.Fatalf("expected ok envelope, got %+v", env)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %T", env.Data)
	}
	if data["dryRun"] != true {
		t.Errorf("expected dryRun true, got %v", data["dryRun"])
	}
	events, ok := data["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("expected progress events in the envelope, got %v", data["events"])
	}
	first := events[0].(map[string]any)
	if first["stage"] == "" || first["status"] == "" {
		t.Errorf("event missing stage/status: %v", first)
	}
	if strings.Contains(out, "ORBIT DEVELOPER WIZARD") {
		t.Errorf("human banner leaked into the JSON envelope: %s", out)
	}
}

func TestOnboardJSONRejectsInteractiveWizard(t *testing.T) {
	originalTTY := onboardIsTTY
	t.Cleanup(func() { onboardIsTTY = originalTTY })
	onboardIsTTY = func() bool { return true }

	out, err := runOnboardCmd(t, "", "--json")
	if err == nil {
		t.Fatal("expected --json on a terminal to fail")
	}
	if exit := clierr.ExitCode(err); exit != 3 {
		t.Errorf("exit code = %d, want 3", exit)
	}
	if !strings.Contains(err.Error(), "interactive wizard") {
		t.Errorf("error should name the interactive wizard: %v", err)
	}

	var env output.Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("expected a JSON failure envelope, got %q: %v", out, err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "usage" {
		t.Errorf("expected a usage failure envelope, got %+v", env)
	}
}

func TestOnboardNonInteractiveFullProgression(t *testing.T) {
	withOnboardAdapters(t, nil)

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")
	sshDir := filepath.Join(tempDir, "ssh")
	_ = os.MkdirAll(workspaceDir, 0755)
	_ = os.MkdirAll(sshDir, 0700)

	secret := testSigningSecret()
	server := newTestEdgeServer(t, secret)
	defer server.Close()

	req := invitedomain.InviteRequest{
		Email:       "test-dev@manova.space",
		DisplayName: "Test Developer",
		Scope:       "core",
		TTL:         24 * time.Hour,
	}
	tokenStr, _, err := invitedomain.GenerateToken(req, secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	out, err := runOnboardCmd(t, "",
		"--non-interactive",
		"--token", tokenStr,
		"--edge-url", server.URL,
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
		"--ssh-dir", sshDir,
		"--skip-stack",
	)
	if err != nil {
		t.Fatalf("onboard command execution failed: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(out, "ONBOARDING COMPLETE") {
		t.Errorf("output missing completion card: %s", out)
	}
	if !strings.Contains(out, "test-dev") {
		t.Errorf("output missing provisioned UID: %s", out)
	}

	sm, err := sessionstore.New(sessionPath)
	if err != nil {
		t.Fatalf("sessionstore.New failed: %v", err)
	}
	s, err := sm.Load()
	if err != nil || s == nil {
		t.Fatalf("failed to load saved session: %v", err)
	}
	if s.CurrentStage != onboarddomain.StageCompleted {
		t.Errorf("expected completed stage, got %s", s.CurrentStage)
	}
	if s.UID != "test-dev" {
		t.Errorf("expected UID test-dev, got %s", s.UID)
	}
	if s.ForgejoToken == "" {
		t.Error("expected the git token to be saved in the session")
	}

	envData, err := os.ReadFile(filepath.Join(workspaceDir, ".cursor", "mcp.env"))
	if err != nil {
		t.Fatalf("expected .cursor/mcp.env to exist: %v", err)
	}
	if !strings.Contains(string(envData), s.ForgejoToken) {
		t.Errorf("expected .cursor/mcp.env to contain the git token")
	}
}

func TestOnboardSessionCheckpointAndResume(t *testing.T) {
	withOnboardAdapters(t, nil)

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")
	sshDir := filepath.Join(tempDir, "ssh")
	_ = os.MkdirAll(workspaceDir, 0755)
	_ = os.MkdirAll(sshDir, 0700)

	secret := testSigningSecret()
	server := newTestEdgeServer(t, secret)
	defer server.Close()

	sm, err := sessionstore.New(sessionPath)
	if err != nil {
		t.Fatalf("sessionstore.New failed: %v", err)
	}
	s := sm.Create("resumed-dev@manova.space", "Resumed Dev")
	s.CurrentStage = onboarddomain.StageKeypairReady
	s.SSHPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICheckpointTestPubKey resumed-dev@manova.space"
	if err := sm.Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	req := invitedomain.InviteRequest{
		Email:       "resumed-dev@manova.space",
		DisplayName: "Resumed Dev",
		Scope:       "core",
		TTL:         24 * time.Hour,
	}
	tokenStr, _, _ := invitedomain.GenerateToken(req, secret)

	out, err := runOnboardCmd(t, "",
		"--resume",
		"--token", tokenStr,
		"--edge-url", server.URL,
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
		"--ssh-dir", sshDir,
		"--non-interactive",
		"--skip-stack",
	)
	if err != nil {
		t.Fatalf("onboard --resume failed: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Resuming existing session") && !strings.Contains(out, "cached checkpoint") {
		t.Errorf("output missing resume indication: %s", out)
	}

	loaded, err := sm.Load()
	if err != nil || loaded == nil {
		t.Fatalf("failed to reload session: %v", err)
	}
	if loaded.CurrentStage != onboarddomain.StageCompleted {
		t.Errorf("expected completed stage, got %s", loaded.CurrentStage)
	}
	if loaded.UID != "resumed-dev" {
		t.Errorf("expected UID resumed-dev, got %s", loaded.UID)
	}
}

func TestOnboardIgnoreAndRemoveCheckpointFlag(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	sessionPath := filepath.Join(tempDir, "session.json")

	sm, err := sessionstore.New(sessionPath)
	if err != nil {
		t.Fatalf("sessionstore.New failed: %v", err)
	}
	s := sm.Create("reset-me@manova.space", "Reset Me")
	s.CurrentStage = onboarddomain.StageTokenClaimed
	if err := sm.Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if !sm.HasPending() {
		t.Fatal("expected a pending session before reset")
	}

	out, err := runOnboardCmd(t, "",
		"--ignore-and-remove-checkpoint",
		"--session-file", sessionPath,
	)
	if err != nil {
		t.Fatalf("onboard --ignore-and-remove-checkpoint failed: %v", err)
	}
	if !strings.Contains(out, "session cleared") {
		t.Errorf("output missing reset confirmation: %s", out)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Errorf("expected the session file to be removed after reset")
	}
}

func TestOnboardRollbackFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

	sm, err := sessionstore.New(sessionPath)
	if err != nil {
		t.Fatalf("sessionstore.New failed: %v", err)
	}
	s := sm.Create("rollback-user@manova.space", "Rollback User")
	s.UID = "rollback-user"
	s.CurrentStage = onboarddomain.StageReposCloned
	s.ClonedRepos = []string{
		filepath.Join("orbit", "orbit-toolkit"),
		filepath.Join("clients", "client-a"),
	}
	if err := sm.Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	out, err := runOnboardCmd(t, "",
		"--rollback",
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
	)
	if err != nil {
		t.Fatalf("onboard --rollback failed: %v", err)
	}
	if !strings.Contains(out, "Rollback completed successfully") {
		t.Errorf("output missing rollback confirmation: %s", out)
	}
	if _, err := os.Stat(repo1); !os.IsNotExist(err) {
		t.Errorf("expected repo1 %s to be deleted by rollback", repo1)
	}
	if _, err := os.Stat(repo2); !os.IsNotExist(err) {
		t.Errorf("expected repo2 %s to be deleted by rollback", repo2)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Errorf("expected the session file to be removed after rollback")
	}
}

func TestOnboardJSONRollbackEnvelope(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	sessionPath := filepath.Join(tempDir, "session.json")

	sm, err := sessionstore.New(sessionPath)
	if err != nil {
		t.Fatalf("sessionstore.New failed: %v", err)
	}
	s := sm.Create("rollback-user@manova.space", "Rollback User")
	s.UID = "rollback-user"
	s.CurrentStage = onboarddomain.StageKeypairReady
	if err := sm.Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	out, err := runOnboardCmd(t, "",
		"--json",
		"--rollback",
		"--session-file", sessionPath,
		"--workspace", filepath.Join(tempDir, "workspace"),
	)
	if err != nil {
		t.Fatalf("onboard --json --rollback failed: %v", err)
	}

	var env output.Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("output is not one JSON envelope: %v\n%s", err, out)
	}
	data, ok := env.Data.(map[string]any)
	if !ok || data["rolledBack"] != true {
		t.Fatalf("expected rolledBack true in %+v", env.Data)
	}
}

func TestOnboardDiagBundle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tempDir := t.TempDir()
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")
	bundlePath := filepath.Join(tempDir, "manova-diag-test.tar.gz")

	withOnboardAdapters(t, func(svc *application.Service) {
		svc.Bundle = &fakeBundle{path: bundlePath}
	})

	sm, err := sessionstore.New(sessionPath)
	if err != nil {
		t.Fatalf("sessionstore.New failed: %v", err)
	}
	s := sm.Create("diag-test@manova.space", "Diag Tester")
	if err := sm.Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	out, err := runOnboardCmd(t, "",
		"--json",
		"--diag-bundle", bundlePath,
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
	)
	if err != nil {
		t.Fatalf("onboard --diag-bundle failed: %v\nOutput: %s", err, out)
	}

	var env output.Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("output is not one JSON envelope: %v\n%s", err, out)
	}
	data, ok := env.Data.(map[string]any)
	if !ok || data["bundlePath"] != bundlePath {
		t.Fatalf("expected bundlePath %q in %+v", bundlePath, env.Data)
	}
}

func TestOnboardAutoFixFlagRegistration(t *testing.T) {
	cmd := newOnboardCmd()

	autoFixFlag := cmd.Flags().Lookup("auto-fix")
	if autoFixFlag == nil {
		t.Fatal("expected --auto-fix flag to be registered")
	}
	if autoFixFlag.Shorthand != "f" {
		t.Errorf("expected shorthand for --auto-fix to be 'f', got %q", autoFixFlag.Shorthand)
	}
	if cmd.Flags().Lookup("fix") == nil {
		t.Fatal("expected --fix flag alias to be registered")
	}

	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil {
		t.Fatal("expected --yes flag to be registered")
	}
	if yesFlag.Shorthand != "y" {
		t.Errorf("expected shorthand for --yes to be 'y', got %q", yesFlag.Shorthand)
	}
	if cmd.Flags().Lookup("non-interactive") == nil {
		t.Fatal("expected --non-interactive flag to be registered")
	}
	if cmd.Flags().Lookup("json") == nil {
		t.Fatal("expected --json flag to be registered")
	}
}

func TestOnboardAutoFixExecutionWithDryRun(t *testing.T) {
	withOnboardAdapters(t, nil)
	t.Setenv("HOME", t.TempDir())

	out, err := runOnboardCmd(t, "",
		"--auto-fix",
		"--dry-run",
		"--session-file", filepath.Join(t.TempDir(), "session.json"),
	)
	if err != nil {
		t.Fatalf("onboard --auto-fix --dry-run failed: %v", err)
	}
	if !strings.Contains(out, "ORBIT DEVELOPER WIZARD") {
		t.Errorf("output missing onboarding banner: %s", out)
	}
	if !strings.Contains(out, "DRY-RUN PREVIEW") {
		t.Errorf("output missing dry run preview card: %s", out)
	}
}

func TestOnboardInteractivePromptDecline(t *testing.T) {
	withOnboardAdapters(t, nil)
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	out, _ := runOnboardCmd(t, "n\nn\n",
		"--session-file", filepath.Join(tempDir, "session.json"),
		"--workspace", filepath.Join(tempDir, "workspace"),
		"--ssh-dir", filepath.Join(tempDir, "ssh"),
	)
	if !strings.Contains(out, "ORBIT DEVELOPER WIZARD") {
		t.Errorf("output missing onboarding banner: %s", out)
	}
}

func TestOnboardTOTPPresentation(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	sessionPath := filepath.Join(tempDir, "session.json")
	workspaceDir := filepath.Join(tempDir, "workspace")
	sshDir := filepath.Join(tempDir, "ssh")
	_ = os.MkdirAll(workspaceDir, 0755)
	_ = os.MkdirAll(sshDir, 0700)

	sm, err := sessionstore.New(sessionPath)
	if err != nil {
		t.Fatalf("sessionstore.New failed: %v", err)
	}
	s := sm.Create("totp-dev@manova.space", "TOTP Dev")
	s.UID = "totp-dev"
	s.CurrentStage = onboarddomain.StageDevStackReady
	s.Metadata = map[string]string{
		"totp_uri": "otpauth://totp/Orbit:totp-dev?secret=JBSWY3DPEHPK3PXP&issuer=Orbit",
	}
	if err := sm.Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	out, err := runOnboardCmd(t, "",
		"--resume",
		"--session-file", sessionPath,
		"--workspace", workspaceDir,
		"--ssh-dir", sshDir,
		"--skip-stack",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("onboard failed: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Authelia 2FA") {
		t.Errorf("expected Authelia 2FA in summary card: %s", out)
	}
	if !strings.Contains(out, "otpauth://totp/Orbit:totp-dev") {
		t.Errorf("expected TOTP URI in summary card: %s", out)
	}
}

// newTestEdgeServer starts the real onboarding edge handler over an in-memory
// listener.
func newTestEdgeServer(t *testing.T, secret []byte) *httptest.Server {
	t.Helper()
	handler, err := server.NewServer(server.ServerConfig{
		Secret:           secret,
		Provisioner:      devprovisioner.NewDevProvisioner(),
		DisableRateLimit: true,
	})
	if err != nil {
		t.Fatalf("server.NewServer failed: %v", err)
	}
	return httptest.NewServer(handler.Handler())
}
