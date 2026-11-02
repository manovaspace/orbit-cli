package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
)

func TestStageOrdering(t *testing.T) {
	if !domain.IsStageCompleted(domain.StageCompleted, domain.StageDoctorPassed) {
		t.Error("completed must satisfy every earlier stage")
	}
	if domain.IsStageCompleted(domain.StageInit, domain.StageDoctorPassed) {
		t.Error("init must not satisfy doctor_passed")
	}
	if !domain.IsStageCompleted(domain.StageKeypairReady, domain.StageKeypairReady) {
		t.Error("a stage satisfies itself")
	}
	for _, stage := range []domain.Stage{
		domain.StageInit,
		domain.StageWelcome,
		domain.StageDoctor,
		domain.StageDoctorPassed,
		domain.StageIdentity,
		domain.StageKeypairReady,
		domain.StageTokenClaimed,
		domain.StageNetworkConfigured,
		domain.StageWorkspace,
		domain.StageReposCloned,
		domain.StageEnvironment,
		domain.StageEnvironmentReady,
		domain.StageMCPConfigured,
		domain.StageStack,
		domain.StageStackReady,
		domain.StageDevStackReady,
		domain.StageComplete,
		domain.StageCompleted,
	} {
		if string(stage) == "" {
			t.Errorf("expected a non-empty stage constant")
		}
	}
}

func TestStepsCoverTheFlow(t *testing.T) {
	if len(domain.Steps) != 7 {
		t.Fatalf("steps = %d, want 7", len(domain.Steps))
	}
	last := domain.Steps[len(domain.Steps)-1]
	if last.Stage != domain.StageDevStackReady {
		t.Errorf("last step = %s, want dev_stack_ready", last.Stage)
	}
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name    string
		opts    domain.Options
		tty     bool
		want    domain.Mode
		wantErr bool
	}{
		{name: "headless", opts: domain.Options{NonInteractive: true}, want: domain.ModeHeadless},
		{name: "prompted without tty", opts: domain.Options{}, want: domain.ModePrompted},
		{name: "wizard on a tty", opts: domain.Options{}, tty: true, want: domain.ModeWizard},
		{name: "dry run is headless", opts: domain.Options{DryRun: true}, tty: true, want: domain.ModeHeadless},
		{name: "json on a tty needs -y", opts: domain.Options{JSON: true}, tty: true, wantErr: true},
		{name: "json with -y", opts: domain.Options{JSON: true, NonInteractive: true}, tty: true, want: domain.ModeHeadless},
		{name: "json with dry run", opts: domain.Options{JSON: true, DryRun: true}, tty: true, want: domain.ModeHeadless},
		{name: "json rollback", opts: domain.Options{JSON: true, Rollback: true}, tty: true, want: domain.ModeHeadless},
		{name: "json bundle", opts: domain.Options{JSON: true, DiagBundle: "default"}, tty: true, want: domain.ModeHeadless},
		{name: "json reset", opts: domain.Options{JSON: true, Reset: true}, tty: true, want: domain.ModeHeadless},
		{name: "json without tty", opts: domain.Options{JSON: true}, want: domain.ModeHeadless},
		{name: "reset is headless", opts: domain.Options{Reset: true}, tty: true, want: domain.ModeHeadless},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, err := domain.ResolveMode(tc.opts, tc.tty)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !strings.Contains(err.Error(), "interactive wizard") {
					t.Errorf("error must name the interactive wizard: %v", err)
				}
				if !strings.Contains(err.Error(), "--non-interactive") {
					t.Errorf("error must name the remedy: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveMode failed: %v", err)
			}
			if mode != tc.want {
				t.Errorf("mode = %s, want %s", mode, tc.want)
			}
		})
	}
}

func TestSessionPending(t *testing.T) {
	s := domain.NewSession("dev@manova.space", "Dev")
	if s.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if !s.Pending() {
		t.Error("a new session is pending")
	}
	s.CurrentStage = domain.StageCompleted
	if s.Pending() {
		t.Error("a completed session is not pending")
	}
	if (*domain.SessionState)(nil).Pending() {
		t.Error("a nil session is not pending")
	}
}

func TestSanitizeSessionMasksSecrets(t *testing.T) {
	s := &domain.SessionState{
		InviteToken:     "orbit-inv.eyJhbGciOiJIUzI1NiJ9.signature",
		ForgejoToken:    "fjo_tok_super_secret",
		WireGuardConfig: "[Interface]\nPrivateKey = secret\nAddress = 10.8.0.5/24\n",
	}
	got := domain.SanitizeSession(s)
	if strings.Contains(got.ForgejoToken, "super_secret") {
		t.Errorf("git token not masked: %s", got.ForgejoToken)
	}
	if got.ForgejoToken != "fjo_tok_***" {
		t.Errorf("git token mask = %q", got.ForgejoToken)
	}
	if !strings.Contains(got.InviteToken, "...") {
		t.Errorf("invite token not masked: %s", got.InviteToken)
	}
	if strings.Contains(got.WireGuardConfig, "secret") {
		t.Errorf("VPN private key not masked: %s", got.WireGuardConfig)
	}
	if !strings.Contains(got.WireGuardConfig, "Address = 10.8.0.5/24") {
		t.Errorf("non-secret VPN lines must stay: %s", got.WireGuardConfig)
	}
	if s.ForgejoToken != "fjo_tok_super_secret" {
		t.Error("SanitizeSession must not mutate its input")
	}

	if domain.SanitizeSession(nil) != nil {
		t.Error("nil session sanitizes to nil")
	}
}

func TestSanitizeEnvContentMasksSensitiveValues(t *testing.T) {
	in := "FORGEJO_TOKEN=fjo_tok_secret\nPORT=10000\n# comment\nAPI_PASSWORD=hunter2\nPLAIN=value\n"
	out := domain.SanitizeEnvContent(in)

	for _, want := range []string{"FORGEJO_TOKEN=[REDACTED]", "PORT=10000", "# comment", "API_PASSWORD=[REDACTED]", "PLAIN=value"} {
		if !strings.Contains(out, want) {
			t.Errorf("sanitized env missing %q:\n%s", want, out)
		}
	}
}

func TestMachineFingerprint(t *testing.T) {
	if got := domain.MachineFingerprint("host", "linux", "amd64"); got != "host-linux-amd64" {
		t.Errorf("fingerprint = %q", got)
	}
	if got := domain.MachineFingerprint("", "linux", "arm64"); got != "unknown-host-linux-arm64" {
		t.Errorf("fingerprint = %q", got)
	}
}

func TestSanitizeUID(t *testing.T) {
	if got := domain.SanitizeUID("  Alex Smith! "); got != "alexsmith" {
		t.Errorf("SanitizeUID = %q", got)
	}
}

func TestProgressEventJSONShape(t *testing.T) {
	event := domain.ProgressEvent{Stage: domain.StageDoctorPassed, Status: domain.StatusCompleted, Message: "ok"}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{`"stage"`, `"status"`, `"message"`, `"timestamp"`} {
		if !strings.Contains(string(payload), field) {
			t.Errorf("event JSON missing %s: %s", field, payload)
		}
	}
}
