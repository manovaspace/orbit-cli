package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/owner/domain"
)

type fakeVault struct {
	rec      *domain.OwnerRecord
	loadErr  error
	saveErr  error
	permsErr error
	exists   bool
	path     string
	saved    *domain.OwnerRecord
}

func (v *fakeVault) Load() (*domain.OwnerRecord, error) { return v.rec, v.loadErr }
func (v *fakeVault) Save(rec *domain.OwnerRecord) error { v.saved = rec; return v.saveErr }
func (v *fakeVault) CheckPermissions() error            { return v.permsErr }
func (v *fakeVault) Exists() bool                       { return v.exists }
func (v *fakeVault) FilePath() string                   { return v.path }

type fakeServer struct {
	initErr     error
	verifyErr   error
	status      string
	initCalls   int
	verifyCalls int
	grantCalls  int
	expiresAt   time.Time
}

func (s *fakeServer) InitiateChallenge(context.Context, string) (domain.ChallengeDispatch, error) {
	s.initCalls++
	return domain.ChallengeDispatch{Email: "owner@example.com"}, s.initErr
}

func (s *fakeServer) VerifyChallenge(context.Context, string, string) (domain.Verification, error) {
	s.verifyCalls++
	return domain.Verification{Status: s.status}, s.verifyErr
}

func (s *fakeServer) CreateGrant(context.Context, string, string, string, string, time.Duration) (domain.GrantRegistration, error) {
	s.grantCalls++
	return domain.GrantRegistration{ExpiresAt: s.expiresAt}, nil
}

type fakeTelegram struct {
	sent int
	err  error
}

func (t *fakeTelegram) SendGrant(context.Context, string, string, string, time.Time) error {
	t.sent++
	return t.err
}

type fakeMailer struct {
	sent int
}

func (m *fakeMailer) SendOwnerChallenge(context.Context, string, ChallengeEmail) error {
	m.sent++
	return nil
}

func newService(vault *fakeVault) (*Service, *bytes.Buffer, *bytes.Buffer) {
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	ui := UI{
		Arrow: "->", OK: "ok", Warn: "warn", Info: "info", Error: "err",
		Title: func(s string) string { return s }, Header: func(s string) string { return s },
		Bold: func(s string) string { return s }, Subtle: func(s string) string { return s },
		Code: func(s string) string { return s }, Success: func(s string) string { return s },
		Warning: func(s string) string { return s }, ErrorStyle: func(s string) string { return s },
		Card: func(title, content string) string { return title + "\n" + content },
	}
	return &Service{
		Vault:     vault,
		Out:       out,
		Err:       errOut,
		Now:       func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		LookupEnv: func(string) string { return "" },
		UI:        ui,
	}, out, errOut
}

func verifiedVault() *fakeVault {
	return &fakeVault{
		exists: true,
		path:   "/tmp/owner.json",
		rec: &domain.OwnerRecord{
			Email:             "admin@example.com",
			DisplayName:       "Admin",
			VerifiedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			RootSigningSecret: "secret",
			KeyFingerprint:    "fingerprint",
		},
	}
}

func TestInitHermeticSealsVault(t *testing.T) {
	vault := &fakeVault{path: "/tmp/owner.json"}
	svc, out, _ := newService(vault)

	res, err := svc.Init(context.Background(), InitInput{Email: "admin@example.com", Code: "123456", NoSend: true})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !res.Verified || res.Email != "admin@example.com" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if vault.saved == nil || len(vault.saved.RootSigningSecret) != 64 {
		t.Fatalf("expected a sealed 64-character secret, got %+v", vault.saved)
	}
	if !strings.Contains(out.String(), "Orbit Platform Ownership Verified") {
		t.Errorf("missing verification output: %s", out.String())
	}
}

func TestInitAlreadyVerifiedWithoutForce(t *testing.T) {
	svc, out, _ := newService(verifiedVault())

	res, err := svc.Init(context.Background(), InitInput{Email: "admin@example.com"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !res.AlreadyVerified {
		t.Fatalf("expected already verified, got %+v", res)
	}
	if !strings.Contains(out.String(), "already verified") {
		t.Errorf("missing already verified line: %s", out.String())
	}
}

func TestInitNoSendWithoutCodeIsUsage(t *testing.T) {
	svc, _, errOut := newService(&fakeVault{path: "/tmp/owner.json"})

	_, err := svc.Init(context.Background(), InitInput{Email: "admin@example.com", NoSend: true})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := clierr.Code(err); got != clierr.CodeUsage {
		t.Errorf("code = %q, want usage", got)
	}
	if !strings.Contains(errOut.String(), "Pre-flight Check Failed") {
		t.Errorf("missing preflight message: %s", errOut.String())
	}
}

func TestInitRemoteChallenge(t *testing.T) {
	vault := &fakeVault{path: "/tmp/owner.json"}
	svc, out, _ := newService(vault)
	server := &fakeServer{status: "verified"}
	svc.Server = server

	res, err := svc.Init(context.Background(), InitInput{
		Email:     "admin@example.com",
		Code:      "748291",
		ServerURL: "https://orbit.example.com",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !res.Verified {
		t.Fatalf("unexpected result: %+v", res)
	}
	if server.initCalls != 1 || server.verifyCalls != 1 {
		t.Errorf("server calls = %d/%d, want 1/1", server.initCalls, server.verifyCalls)
	}
	if !strings.Contains(out.String(), "Connecting to Orbit server at https://orbit.example.com") {
		t.Errorf("missing connection line: %s", out.String())
	}
}

func TestInitRemoteRejectionIsFailed(t *testing.T) {
	svc, _, _ := newService(&fakeVault{path: "/tmp/owner.json"})
	svc.Server = &fakeServer{status: "pending"}

	_, err := svc.Init(context.Background(), InitInput{Email: "admin@example.com", Code: "123456"})
	if err == nil || !strings.Contains(err.Error(), "server rejected verification") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := clierr.Code(err); got != clierr.CodeFailed {
		t.Errorf("code = %q, want failed", got)
	}
}

func TestVerifySealsLocally(t *testing.T) {
	vault := &fakeVault{path: "/tmp/owner.json"}
	svc, out, _ := newService(vault)

	res, err := svc.Verify(context.Background(), VerifyInput{Email: "owner@example.com", Code: "654321", DisplayName: "Owner"})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Verified || vault.saved == nil {
		t.Fatalf("expected a sealed record, got %+v", res)
	}
	if !strings.Contains(out.String(), "verified and sealed in vault") {
		t.Errorf("missing verify output: %s", out.String())
	}
}

func TestStatusReportsVerifiedVault(t *testing.T) {
	svc, _, _ := newService(verifiedVault())

	res := svc.Status()
	if !res.Verified || res.Email != "admin@example.com" {
		t.Fatalf("unexpected status: %+v", res)
	}
	if !res.PermissionsValid || res.VaultPermissions != "0600 (secure)" {
		t.Errorf("unexpected permissions: %+v", res)
	}
}

func TestStatusInsecurePermissions(t *testing.T) {
	vault := verifiedVault()
	vault.permsErr = domain.ErrInsecurePermissions
	svc, _, _ := newService(vault)

	res := svc.Status()
	if res.PermissionsValid {
		t.Fatal("expected invalid permissions")
	}
	if !strings.Contains(res.VaultPermissions, "insecure") {
		t.Errorf("permissions = %q", res.VaultPermissions)
	}
}

func TestRotateSecretReplacesSecret(t *testing.T) {
	vault := verifiedVault()
	svc, out, _ := newService(vault)

	res, err := svc.RotateSecret(context.Background(), RotateInput{Yes: true})
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if vault.saved == nil || vault.saved.RootSigningSecret == "secret" {
		t.Fatal("expected a new secret")
	}
	if res.OldFingerprint != "fingerprint" || res.KeyFingerprint == "" {
		t.Errorf("unexpected fingerprints: %+v", res)
	}
	if !strings.Contains(out.String(), "invalidated") {
		t.Errorf("missing invalidation warning: %s", out.String())
	}
}

func TestRotateSecretUnverifiedIsAuth(t *testing.T) {
	svc, _, _ := newService(&fakeVault{path: "/tmp/owner.json"})

	_, err := svc.RotateSecret(context.Background(), RotateInput{Yes: true})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := clierr.Code(err); got != clierr.CodeAuth {
		t.Errorf("code = %q, want auth", got)
	}
}

func TestGrantRegistersAndDispatches(t *testing.T) {
	svc, out, _ := newService(verifiedVault())
	server := &fakeServer{expiresAt: time.Date(2026, 1, 2, 3, 30, 0, 0, time.UTC)}
	svc.Server = server
	telegram := &fakeTelegram{}
	svc.Telegram = telegram
	mailer := &fakeMailer{}
	svc.MailerFactory = func(SMTPConfig) Mailer { return mailer }

	res, err := svc.Grant(context.Background(), GrantInput{
		Email:     "sara@manova.space",
		Role:      "admin",
		TTL:       30 * time.Minute,
		Send:      true,
		Telegram:  true,
		ServerURL: "https://orbit.example.com",
	})
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if server.grantCalls != 1 || telegram.sent != 1 || mailer.sent != 1 {
		t.Errorf("calls = %d/%d/%d, want 1/1/1", server.grantCalls, telegram.sent, mailer.sent)
	}
	if res.Code == "" || !strings.Contains(res.Code, "-") {
		t.Errorf("code = %q", res.Code)
	}
	if !res.TelegramSent || !res.EmailSent {
		t.Errorf("unexpected dispatch flags: %+v", res)
	}
	if !strings.Contains(out.String(), "Orbit Administrator Grant Generated") {
		t.Errorf("missing grant output: %s", out.String())
	}
}

func TestGrantUnverifiedIsAuth(t *testing.T) {
	svc, _, _ := newService(&fakeVault{path: "/tmp/owner.json"})

	_, err := svc.Grant(context.Background(), GrantInput{Email: "sara@manova.space"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := clierr.Code(err); got != clierr.CodeAuth {
		t.Errorf("code = %q, want auth", got)
	}
}

func TestGrantInvalidCodeIsUsage(t *testing.T) {
	svc, _, _ := newService(verifiedVault())

	_, err := svc.Grant(context.Background(), GrantInput{Email: "sara@manova.space", Code: "123"})
	if err == nil || clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResetTOTPRequiresVerifiedOwner(t *testing.T) {
	svc, _, _ := newService(verifiedVault())

	res, err := svc.ResetTOTP(context.Background(), ResetTOTPInput{Email: "user@manova.space"})
	if err != nil {
		t.Fatalf("ResetTOTP: %v", err)
	}
	if res.RecoveryCode == "" || res.ExpiresIn != "15m" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestInitWrapSaveError(t *testing.T) {
	vault := &fakeVault{path: "/tmp/owner.json", saveErr: errors.New("disk full")}
	svc, _, _ := newService(vault)

	_, err := svc.Init(context.Background(), InitInput{Email: "admin@example.com", Code: "123456", NoSend: true})
	if err == nil || !strings.Contains(err.Error(), "failed to save owner record") {
		t.Fatalf("unexpected error: %v", err)
	}
}
