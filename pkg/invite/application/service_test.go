package application

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/invite/domain"
)

type fakeStore struct {
	records []*domain.InviteRecord
	saveErr error
}

func (s *fakeStore) SaveInvite(rec *domain.InviteRecord) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.records = append(s.records, rec)
	return nil
}

func (s *fakeStore) ListInvites() ([]*domain.InviteRecord, error) { return s.records, nil }

func (s *fakeStore) GetInvite(tokenOrID string) (*domain.InviteRecord, error) {
	for _, rec := range s.records {
		if rec.ID == tokenOrID || rec.Token == tokenOrID {
			return rec, nil
		}
	}
	return nil, domain.ErrInviteNotFound
}

func (s *fakeStore) RevokeInvite(tokenOrID string) (*domain.InviteRecord, error) {
	rec, err := s.GetInvite(tokenOrID)
	if err != nil {
		return nil, err
	}
	rec.Revoked = true
	return rec, nil
}

func (s *fakeStore) RevokeAllInvites() ([]*domain.InviteRecord, error) {
	var revoked []*domain.InviteRecord
	for _, rec := range s.records {
		if !rec.Revoked {
			rec.Revoked = true
			revoked = append(revoked, rec)
		}
	}
	return revoked, nil
}

type fakeMailer struct {
	sent int
}

func (m *fakeMailer) SendInvite(context.Context, string, domain.EmailData) error {
	m.sent++
	return nil
}

type fakeOwner struct {
	email    string
	secret   string
	verified bool
}

func (o fakeOwner) Owner() (string, string, bool) { return o.email, o.secret, o.verified }

func newInviteService(store Store, owner OwnerVault) (*Service, *bytes.Buffer, *bytes.Buffer) {
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	return &Service{
		Store:     store,
		Owner:     owner,
		LookupEnv: func(string) string { return "" },
		Now:       func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		Out:       out,
		Err:       errOut,
		UI: UI{
			Warn:    "warn",
			Warning: func(s string) string { return s },
		},
	}, out, errOut
}

func TestCreateSignsAndStoresInvite(t *testing.T) {
	store := &fakeStore{}
	svc, _, _ := newInviteService(store, fakeOwner{email: "admin@example.com", secret: "owner-secret", verified: true})
	mailer := &fakeMailer{}

	res, err := svc.Create(context.Background(), CreateInput{
		Email:       "dev@example.com",
		DisplayName: "Dev",
		Scope:       "core",
		Expires:     "48h",
		Send:        true,
		Mailer:      mailer,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(store.records) != 1 {
		t.Fatalf("expected one stored invite, got %d", len(store.records))
	}
	if !strings.HasPrefix(res.Token, "orbit-inv.") {
		t.Errorf("token = %q", res.Token)
	}
	if res.CreatedBy != "admin@example.com" {
		t.Errorf("createdBy = %q", res.CreatedBy)
	}
	if res.FallbackSecret {
		t.Error("expected the owner secret, not the fallback")
	}
	if mailer.sent != 1 || !res.MailDispatched {
		t.Errorf("expected one dispatched mail, got %d", mailer.sent)
	}
	if _, err := domain.ValidateToken(res.Token, []byte("owner-secret")); err != nil {
		t.Fatalf("token does not validate with the owner secret: %v", err)
	}
}

func TestCreateUnverifiedOwnerIsAuth(t *testing.T) {
	svc, _, _ := newInviteService(&fakeStore{}, fakeOwner{})

	_, err := svc.Create(context.Background(), CreateInput{Email: "dev@example.com", Expires: "24h"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := clierr.Code(err); got != clierr.CodeAuth {
		t.Errorf("code = %q, want auth", got)
	}
}

func TestCreateInsecureBypassesOwnerCheck(t *testing.T) {
	svc, _, errOut := newInviteService(&fakeStore{}, fakeOwner{})

	res, err := svc.Create(context.Background(), CreateInput{Email: "dev@example.com", Expires: "24h", Insecure: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !res.FallbackSecret {
		t.Error("expected the fallback secret warning")
	}
	if !strings.Contains(errOut.String(), "Using default dev signing secret") {
		t.Errorf("missing fallback warning: %s", errOut.String())
	}
}

func TestCreateInvalidEmailIsUsage(t *testing.T) {
	svc, _, _ := newInviteService(&fakeStore{}, fakeOwner{secret: "x", verified: true})

	_, err := svc.Create(context.Background(), CreateInput{Email: "not-an-email", Expires: "24h"})
	if err == nil || clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateTOTPEmbeddedInMetadata(t *testing.T) {
	store := &fakeStore{}
	svc, _, _ := newInviteService(store, fakeOwner{secret: "owner-secret", verified: true})

	res, err := svc.Create(context.Background(), CreateInput{Email: "dev@example.com", Expires: "24h", TOTP: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.TOTPSecret == "" || !strings.HasPrefix(res.TOTPURI, "otpauth://totp/") {
		t.Errorf("unexpected TOTP fields: %+v", res)
	}
	if store.records[0].Metadata["otpauth"] == "" {
		t.Error("expected otpauth metadata on the stored record")
	}
}

func TestListFiltersActiveUnlessAll(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{records: []*domain.InviteRecord{
		{ID: "active", ExpiresAt: now.Add(time.Hour)},
		{ID: "revoked", Revoked: true, ExpiresAt: now.Add(time.Hour)},
		{ID: "expired", ExpiresAt: now.Add(-time.Hour)},
	}}
	svc, _, _ := newInviteService(store, fakeOwner{})

	active, err := svc.List(false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(active.Displayed) != 1 || active.Displayed[0].ID != "active" {
		t.Errorf("unexpected active list: %+v", active.Displayed)
	}
	if active.Total != 3 || active.Active != 1 || active.Revoked != 1 || active.Expired != 1 {
		t.Errorf("unexpected counters: %+v", active)
	}

	all, err := svc.List(true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all.Displayed) != 3 {
		t.Errorf("expected all three records, got %d", len(all.Displayed))
	}
}

func TestRevokeOneAndAll(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{records: []*domain.InviteRecord{
		{ID: "one", ExpiresAt: now.Add(time.Hour)},
		{ID: "two", ExpiresAt: now.Add(time.Hour)},
	}}
	svc, _, _ := newInviteService(store, fakeOwner{})

	res, err := svc.Revoke("one", false)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if len(res.Revoked) != 1 || res.Revoked[0].ID != "one" {
		t.Errorf("unexpected revocation: %+v", res.Revoked)
	}

	res, err = svc.Revoke("", true)
	if err != nil {
		t.Fatalf("Revoke all: %v", err)
	}
	if len(res.Revoked) != 1 || res.Revoked[0].ID != "two" {
		t.Errorf("unexpected bulk revocation: %+v", res.Revoked)
	}

	res, err = svc.Revoke("", true)
	if err != nil {
		t.Fatalf("Revoke all: %v", err)
	}
	if !res.None {
		t.Error("expected no active invitations")
	}
}

func TestRevokeMissingInviteIsFailed(t *testing.T) {
	svc, _, _ := newInviteService(&fakeStore{}, fakeOwner{})

	_, err := svc.Revoke("missing-id", false)
	if err == nil || !strings.Contains(err.Error(), "not found in store") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := clierr.Code(err); got != clierr.CodeFailed {
		t.Errorf("code = %q, want failed", got)
	}
}

func TestRevokeArgsConflictIsUsage(t *testing.T) {
	svc, _, _ := newInviteService(&fakeStore{}, fakeOwner{})

	_, err := svc.Revoke("token", true)
	if err == nil || clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIssueUsesExplicitSecret(t *testing.T) {
	store := &fakeStore{}
	svc, _, _ := newInviteService(store, fakeOwner{})

	res, err := svc.Issue(context.Background(), IssueInput{
		Email:  "dev@example.com",
		Scope:  "core",
		TTL:    time.Hour,
		Secret: []byte("explicit-secret"),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := domain.ValidateToken(res.Record.Token, []byte("explicit-secret")); err != nil {
		t.Fatalf("token does not validate: %v", err)
	}
	if res.Record.Scope != "core" {
		t.Errorf("scope = %q", res.Record.Scope)
	}
}
