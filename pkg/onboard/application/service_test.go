package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

// fakeProvisioner answers claims and records the request it received.
type fakeProvisioner struct {
	response domain.ClaimResponse
	claimErr error

	claims      int
	rollbacks   int
	edgeURL     string
	idempotency string
	lastRequest domain.ClaimRequest
}

func (f *fakeProvisioner) Claim(_ context.Context, edgeURL, idempotencyKey string, req domain.ClaimRequest) (*domain.ClaimResponse, error) {
	f.claims++
	f.edgeURL = edgeURL
	f.idempotency = idempotencyKey
	f.lastRequest = req
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	resp := f.response
	if resp.User.UID == "" {
		resp.User = domain.User{UID: req.DesiredUID, Email: req.Email, DisplayName: req.DisplayName}
	}
	return &resp, nil
}

func (f *fakeProvisioner) Rollback(_ context.Context, edgeURL, uid string) error {
	f.rollbacks++
	f.edgeURL = edgeURL
	return nil
}

// fakeSessions is an in-memory checkpoint store.
type fakeSessions struct {
	state   *domain.SessionState
	cleared int
	saves   int
}

func (f *fakeSessions) HasPending() bool { return f.state.Pending() }

func (f *fakeSessions) Load() (*domain.SessionState, error) { return f.state, nil }

func (f *fakeSessions) Save(state *domain.SessionState) error {
	f.saves++
	f.state = state
	return nil
}

func (f *fakeSessions) Clear() error {
	f.cleared++
	f.state = nil
	return nil
}

func (f *fakeSessions) Create(email, name string) *domain.SessionState {
	return domain.NewSession(email, name)
}

// fakeEnvironment records every write instead of touching the disk.
type fakeEnvironment struct {
	home      string
	exists    map[string]bool
	dirs      map[string]bool
	writes    []string
	removed   []string
	dns       map[string][]string
	caPresent bool
	caTrusted bool
	env       map[string]string
}

func newFakeEnvironment() *fakeEnvironment {
	return &fakeEnvironment{
		home:   "/home/dev",
		exists: map[string]bool{},
		dirs:   map[string]bool{},
		dns:    map[string][]string{},
		env:    map[string]string{},
	}
}

func (f *fakeEnvironment) HomeDir() (string, error) { return f.home, nil }
func (f *fakeEnvironment) Hostname() string         { return "testhost" }
func (f *fakeEnvironment) Platform() (string, string) {
	return "linux", "amd64"
}
func (f *fakeEnvironment) Getenv(key string) string { return f.env[key] }

func (f *fakeEnvironment) LookupHost(name string) ([]string, error) {
	if addrs, ok := f.dns[name]; ok {
		return addrs, nil
	}
	return nil, errors.New("no such host")
}

func (f *fakeEnvironment) TrustDevCA(context.Context) (bool, bool) {
	return f.caPresent, f.caTrusted
}

func (f *fakeEnvironment) EnsureWorkspaceDirs(string) error { return nil }

func (f *fakeEnvironment) WriteWireGuard(string) (string, error) {
	f.writes = append(f.writes, "wireguard")
	return f.home + "/.config/orbit/wg0.conf", nil
}

func (f *fakeEnvironment) ConfigureMCPEnv(string, string, string) error {
	f.writes = append(f.writes, "mcp.env")
	return nil
}

func (f *fakeEnvironment) ConfigureShell(string) error {
	f.writes = append(f.writes, "shell")
	return nil
}

func (f *fakeEnvironment) Exists(path string) bool { return f.exists[path] }

func (f *fakeEnvironment) IsDir(path string) bool { return f.dirs[path] }

func (f *fakeEnvironment) Remove(path string) error {
	f.removed = append(f.removed, path)
	return nil
}

// fakeDiagnostics returns a scripted report.
type fakeDiagnostics struct {
	report    *doctor.DoctorReport
	healable  int
	healed    int
	healCalls int
}

func (f *fakeDiagnostics) Run() *doctor.DoctorReport { return f.report }

func (f *fakeDiagnostics) HealableCount([]doctor.DiagnosticResult) int { return f.healable }

func (f *fakeDiagnostics) Heal(_ context.Context, _ []doctor.DiagnosticResult, progress func(string, string)) error {
	f.healCalls++
	if progress != nil {
		progress("Go toolchain", "Completed successfully")
	}
	f.healed++
	return nil
}

// fakeSSH returns a fixed key or an error.
type fakeSSH struct {
	key string
	err error
}

func (f fakeSSH) EnsureKey(string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.key == "" {
		return "ssh-ed25519 AAAATestKey dev@test", nil
	}
	return f.key, nil
}

// fakePrompts answers questions from a script.
type fakePrompts struct {
	yesNo  []bool
	lines  []string
	asked  []string
	index  int
	lineAt int
}

func (f *fakePrompts) YesNo(question, _ string, _ bool) bool {
	f.asked = append(f.asked, question)
	if f.index >= len(f.yesNo) {
		return true
	}
	answer := f.yesNo[f.index]
	f.index++
	return answer
}

func (f *fakePrompts) String(question, _, _ string) string {
	f.asked = append(f.asked, question)
	if f.lineAt >= len(f.lines) {
		return ""
	}
	answer := f.lines[f.lineAt]
	f.lineAt++
	return answer
}

// fakeRepos resolves one repository and reports it as cloned.
type fakeRepos struct {
	rels        []string
	cloneErr    error
	pulled      []string
	pullErr     error
	resultCount int
}

func (f *fakeRepos) CloneCore(_ context.Context, _ string, _ string, onStart func(int), onResult func(workspacedomain.CloneResult)) ([]string, error) {
	if f.cloneErr != nil {
		return nil, f.cloneErr
	}
	targets := f.rels
	if len(targets) == 0 {
		targets = []string{"orbit/orbit-cli"}
	}
	if onStart != nil {
		onStart(len(targets))
	}
	for _, rel := range targets {
		f.resultCount++
		if onResult != nil {
			onResult(workspacedomain.CloneResult{Name: rel, Path: rel, Success: true})
		}
	}
	return targets, nil
}

func (f *fakeRepos) PullAssets(_ context.Context, _ string, rels []string) error {
	f.pulled = append(f.pulled, rels...)
	return f.pullErr
}

func newTestService() *Service {
	return &Service{
		Provisioner: &fakeProvisioner{response: domain.ClaimResponse{
			User: domain.User{UID: "test-dev", Email: "test-dev@manova.space", DisplayName: "Test Dev"},
			Credentials: domain.Credentials{
				ForgejoUsername: "test-dev",
				ForgejoMCPToken: "fjo_tok_secret_value", // gitleaks:allow
				WireGuardConfig: "[Interface]\nPrivateKey = secret_key\nAddress = 10.8.0.5/24\n",
			},
		}},
		Sessions:    &fakeSessions{},
		Diagnostics: &fakeDiagnostics{report: &doctor.DoctorReport{Results: diagnosticOK()}},
		SSH:         fakeSSH{},
		Environment: newFakeEnvironment(),
		Layout:      LayoutStub{},
		Repos:       &fakeRepos{},
		Prompts:     &fakePrompts{},
		Out:         new(bytes.Buffer),
		Now:         func() time.Time { return time.Unix(1700000000, 0) },
	}
}

// LayoutStub resolves fixed locations.
type LayoutStub struct{}

func (LayoutStub) WorkspaceRoot(string) string        { return "/workspace" }
func (LayoutStub) ManifestPath(string, string) string { return "/workspace/workspace.yaml" }
func (LayoutStub) InfraDir(string) string             { return "/workspace/orbit/orbit-infra" }

func diagnosticOK() []doctor.DiagnosticResult {
	return []doctor.DiagnosticResult{
		{Category: doctor.CategoryCore, Name: "Git", Status: doctor.StatusOK, Message: "git is installed"},
	}
}

func headlessOptions() domain.Options {
	return domain.Options{NonInteractive: true, SkipStack: true, Token: "test-invite-token"}
}

func TestRunHeadlessCompletesEveryStep(t *testing.T) {
	svc := newTestService()
	svc.Environment.(*fakeEnvironment).exists["/workspace/workspace.yaml"] = true
	svc.Environment.(*fakeEnvironment).dirs["/workspace/orbit/orbit-infra"] = true
	svc.Prompts = &fakePrompts{}

	res, err := svc.Run(context.Background(), headlessOptions())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if res.Stage != domain.StageCompleted {
		t.Errorf("stage = %s, want completed", res.Stage)
	}
	if res.UID != "test-dev" || res.Email != "test-dev@manova.space" {
		t.Errorf("unexpected identity in result: %+v", res)
	}
	if res.Workspace != "/workspace" {
		t.Errorf("workspace = %q, want /workspace", res.Workspace)
	}
	if len(res.ClonedRepos) != 1 || res.ClonedRepos[0] != "orbit/orbit-cli" {
		t.Errorf("unexpected cloned repos: %v", res.ClonedRepos)
	}

	out := svc.Out.(*bytes.Buffer).String()
	for _, want := range []string{
		"ORBIT DEVELOPER WIZARD",
		"── Step 1: Pre-Flight System Diagnostics ──────────────────",
		"── Step 2: SSH Developer Keypair ──────────────────────────",
		"Identity provisioned for",
		"ONBOARDING COMPLETE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	events := res.Events
	if len(events) == 0 {
		t.Fatal("expected progress events")
	}
	last := events[len(events)-1]
	if last.Stage != domain.StageCompleted || last.Status != domain.StatusCompleted {
		t.Errorf("last event = %+v, want completed", last)
	}

	session := svc.Sessions.(*fakeSessions).state
	if session == nil || session.CurrentStage != domain.StageCompleted {
		t.Fatalf("session was not completed: %+v", session)
	}
	if session.ForgejoToken != "fjo_tok_secret_value" {
		t.Errorf("session missing git token")
	}
	if svc.SSH == nil {
		t.Error("ssh adapter missing")
	}
	if got := svc.Environment.(*fakeEnvironment).writes; len(got) == 0 {
		t.Errorf("expected MCP/shell/VPN writes, got %v", got)
	}
}

func TestRunDryRunStopsAfterDiagnostics(t *testing.T) {
	svc := newTestService()
	opts := domain.Options{NonInteractive: true, DryRun: true}

	res, err := svc.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.DryRun {
		t.Error("expected dryRun in the result")
	}
	if svc.Provisioner.(*fakeProvisioner).claims != 0 {
		t.Error("dry run must not claim the token")
	}

	out := svc.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "DRY-RUN PREVIEW") {
		t.Errorf("output missing dry-run preview: %s", out)
	}
	if strings.Contains(out, "ONBOARDING COMPLETE") {
		t.Errorf("dry run must not print the completion card")
	}
}

func TestRunClaimsWithIdempotencyAndEdgeURL(t *testing.T) {
	svc := newTestService()
	env := svc.Environment.(*fakeEnvironment)
	env.env["ORBIT_SERVER"] = "https://edge.example"

	res, err := svc.Run(context.Background(), domain.Options{
		NonInteractive: true,
		SkipStack:      true,
		Token:          "invite-token-value",
		UID:            "chosen",
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	prov := svc.Provisioner.(*fakeProvisioner)
	if prov.edgeURL != "https://edge.example" {
		t.Errorf("edge URL = %q, want $ORBIT_SERVER", prov.edgeURL)
	}
	if prov.idempotency == "" {
		t.Error("expected an idempotency key")
	}
	if prov.lastRequest.InviteToken != "invite-token-value" || prov.lastRequest.DesiredUID != "chosen" {
		t.Errorf("unexpected claim request: %+v", prov.lastRequest)
	}
	if prov.lastRequest.MachineFingerprint != "testhost-linux-amd64" {
		t.Errorf("fingerprint = %q", prov.lastRequest.MachineFingerprint)
	}
	if res.Stage != domain.StageCompleted {
		t.Errorf("stage = %s", res.Stage)
	}
}

func TestRunMissingTokenInHeadlessModeIsUsage(t *testing.T) {
	svc := newTestService()

	_, err := svc.Run(context.Background(), domain.Options{NonInteractive: true, SkipStack: true})
	if err == nil {
		t.Fatal("expected an error without a token")
	}
	if code := clierr.Code(err); code != clierr.CodeUsage {
		t.Errorf("code = %q, want usage", code)
	}
}

func TestRunClaimFailureKeepsNetworkCode(t *testing.T) {
	svc := newTestService()
	svc.Provisioner.(*fakeProvisioner).claimErr = clierr.Network("claim rejected with HTTP 502")

	_, err := svc.Run(context.Background(), domain.Options{
		NonInteractive: true,
		SkipStack:      true,
		Token:          "token",
	})
	if err == nil {
		t.Fatal("expected the claim error")
	}
	if code := clierr.Code(err); code != clierr.CodeNetwork {
		t.Errorf("code = %q, want network", code)
	}
	if !strings.Contains(err.Error(), "onboarding claim failed") {
		t.Errorf("error should name the claim step: %v", err)
	}
}

func TestRunPreflightErrorsFailNonInteractive(t *testing.T) {
	svc := newTestService()
	svc.Diagnostics = &fakeDiagnostics{report: &doctor.DoctorReport{Results: []doctor.DiagnosticResult{
		{Name: "Docker", Status: doctor.StatusError, Message: "docker is missing", FixSuggestion: "install docker"},
	}}}

	_, err := svc.Run(context.Background(), headlessOptions())
	if err == nil {
		t.Fatal("expected pre-flight failure")
	}
	if code := clierr.Code(err); code != clierr.CodeFailed {
		t.Errorf("code = %q, want failed", code)
	}
	if !strings.Contains(err.Error(), "pre-flight diagnostics failed with 1 error(s)") {
		t.Errorf("unexpected message: %v", err)
	}
}

func TestRunAutoHealsInNonInteractiveMode(t *testing.T) {
	svc := newTestService()
	diag := &fakeDiagnostics{
		report:   &doctor.DoctorReport{Results: diagnosticOK()},
		healable: 1,
	}
	svc.Diagnostics = diag

	out := svc.Out.(*bytes.Buffer)
	opts := headlessOptions()
	opts.AutoFix = true

	if _, err := svc.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if diag.healCalls != 1 {
		t.Errorf("heal calls = %d, want 1", diag.healCalls)
	}
	if !strings.Contains(out.String(), "Auto-Healing") {
		t.Errorf("output missing auto-healing heading: %s", out.String())
	}
}

func TestRunJSONSuppressesHumanOutputAndKeepsEvents(t *testing.T) {
	svc := newTestService()
	svc.Out = new(bytes.Buffer)
	opts := domain.Options{NonInteractive: true, SkipStack: true, JSON: true, DryRun: true}

	res, err := svc.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got := svc.Out.(*bytes.Buffer).String(); got != "" {
		t.Errorf("JSON mode wrote human output: %q", got)
	}
	if len(res.Events) == 0 {
		t.Fatal("expected events in JSON mode")
	}

	payload, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, secret := range []string{"fjo_tok_secret_value", "secret_key", "invite-token-value"} {
		if strings.Contains(string(payload), secret) {
			t.Errorf("result leaked a secret %q: %s", secret, payload)
		}
	}
}

func TestRunResumeFromCheckpoint(t *testing.T) {
	svc := newTestService()
	state := domain.NewSession("resumed@manova.space", "Resumed")
	state.CurrentStage = domain.StageKeypairReady
	state.SSHPublicKey = "ssh-ed25519 AAAAExisting"
	state.UID = "resumed"
	state.ForgejoToken = "existing-token"
	svc.Sessions.(*fakeSessions).state = state

	res, err := svc.Run(context.Background(), domain.Options{
		NonInteractive: true,
		SkipStack:      true,
		Resume:         true,
		Token:          "token",
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.UID != "test-dev" {
		t.Errorf("expected the claim to update the UID, got %q", res.UID)
	}
	out := svc.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "Resuming existing session") {
		t.Errorf("output missing resume line: %s", out)
	}
	if !strings.Contains(out, "SSH keypair ready (cached checkpoint)") {
		t.Errorf("expected the keypair step to be skipped: %s", out)
	}
}

func TestRunResetClearsCheckpoint(t *testing.T) {
	svc := newTestService()
	svc.Sessions.(*fakeSessions).state = domain.NewSession("dev@manova.space", "Dev")

	res, err := svc.Run(context.Background(), domain.Options{Reset: true})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.Reset {
		t.Error("expected reset in the result")
	}
	if svc.Sessions.(*fakeSessions).cleared == 0 {
		t.Error("expected the checkpoint to be cleared")
	}
	if !strings.Contains(svc.Out.(*bytes.Buffer).String(), "session cleared") {
		t.Errorf("output missing the reset confirmation")
	}
}

func TestRunRollbackRemovesProvisionedFiles(t *testing.T) {
	svc := newTestService()
	env := svc.Environment.(*fakeEnvironment)
	env.dirs["/workspace/orbit/orbit-cli"] = true
	env.exists["/home/dev/.config/orbit/wg0.conf"] = true
	env.exists["/workspace/.cursor/mcp.env"] = true

	state := domain.NewSession("dev@manova.space", "Dev")
	state.UID = "dev"
	state.ClonedRepos = []string{"orbit/orbit-cli"}
	svc.Sessions.(*fakeSessions).state = state

	res, err := svc.Run(context.Background(), domain.Options{Rollback: true, EdgeURL: "http://edge"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.RolledBack {
		t.Error("expected rolledBack in the result")
	}
	if svc.Provisioner.(*fakeProvisioner).rollbacks != 1 {
		t.Error("expected the remote rollback to be requested")
	}
	if len(env.removed) != 3 {
		t.Errorf("removed paths = %v, want repository, VPN profile and MCP env", env.removed)
	}
	if svc.Sessions.(*fakeSessions).state != nil {
		t.Error("expected the session file to be cleared")
	}
	if !strings.Contains(svc.Out.(*bytes.Buffer).String(), "Rollback completed successfully") {
		t.Errorf("output missing rollback confirmation")
	}
}

func TestRunRollbackWithoutSessionIsANoop(t *testing.T) {
	svc := newTestService()
	res, err := svc.Run(context.Background(), domain.Options{Rollback: true})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.RolledBack {
		t.Error("nothing was rolled back")
	}
	if !strings.Contains(svc.Out.(*bytes.Buffer).String(), "No active onboarding session") {
		t.Errorf("output missing the no-session notice")
	}
}

func TestRunDiagnosticBundleRecordsPath(t *testing.T) {
	svc := newTestService()
	svc.Bundle = bundleStub{path: "/tmp/bundle.tar.gz"}

	res, err := svc.Run(context.Background(), domain.Options{DiagBundle: "default", BundleArg: "custom.tar.gz"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.BundlePath != "/tmp/bundle.tar.gz" {
		t.Errorf("bundle path = %q", res.BundlePath)
	}
}

type bundleStub struct{ path string }

func (b bundleStub) Write(string, *domain.SessionState, string) (string, error) { return b.path, nil }

func TestRunSkipsCachedStages(t *testing.T) {
	svc := newTestService()
	state := domain.NewSession("dev@manova.space", "Dev")
	state.CurrentStage = domain.StageMCPConfigured
	state.UID = "dev"
	svc.Sessions.(*fakeSessions).state = state

	if _, err := svc.Run(context.Background(), domain.Options{Resume: true, NonInteractive: true, SkipStack: true}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	out := svc.Out.(*bytes.Buffer).String()
	for _, want := range []string{
		"Pre-flight diagnostics already passed (cached checkpoint)",
		"SSH keypair ready (cached checkpoint)",
		"Identity claimed: dev (cached checkpoint)",
		"Network & TLS setup ready (cached checkpoint)",
		"Workspace repositories ready (cached checkpoint)",
		"MCP environment ready (cached checkpoint)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if svc.Provisioner.(*fakeProvisioner).claims != 0 {
		t.Error("cached stages must not claim")
	}
}
