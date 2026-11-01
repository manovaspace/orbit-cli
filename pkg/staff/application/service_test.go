package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/staff/domain"
)

type fakeDirectory struct {
	created   *domain.CreateInput
	createRes *domain.CreateResult
	createErr error
	members   []domain.Member
	updateRes *domain.Member
	updateErr error
	lastUID   string
	resetLDAP bool
	resetMail bool
}

func (d *fakeDirectory) Create(_ context.Context, in domain.CreateInput) (*domain.CreateResult, error) {
	d.created = &in
	if d.createErr != nil {
		return nil, d.createErr
	}
	if d.createRes != nil {
		return d.createRes, nil
	}
	return &domain.CreateResult{Member: domain.Member{UID: in.UID}}, nil
}

func (d *fakeDirectory) List(context.Context) ([]domain.Member, error) { return d.members, nil }

func (d *fakeDirectory) Get(_ context.Context, uid string) (*domain.Member, error) {
	for _, m := range d.members {
		if m.UID == uid {
			return &m, nil
		}
	}
	return nil, clierr.Failed("not found")
}

func (d *fakeDirectory) Update(_ context.Context, uid string, in domain.UpdateInput) (*domain.Member, error) {
	d.lastUID = uid
	if d.updateErr != nil {
		return nil, d.updateErr
	}
	if d.updateRes != nil {
		return d.updateRes, nil
	}
	return &domain.Member{UID: uid, FirstName: in.FirstName, LastName: in.LastName}, nil
}

func (d *fakeDirectory) Disable(_ context.Context, uid string) error { d.lastUID = uid; return nil }
func (d *fakeDirectory) Enable(_ context.Context, uid string) error  { d.lastUID = uid; return nil }
func (d *fakeDirectory) Delete(_ context.Context, uid string) error  { d.lastUID = uid; return nil }

func (d *fakeDirectory) ResetPassword(_ context.Context, uid string, ldap, mailbox bool) (*domain.ResetResult, error) {
	d.lastUID = uid
	d.resetLDAP, d.resetMail = ldap, mailbox
	return &domain.ResetResult{LDAPPassword: "rotated"}, nil
}

type fakeVault struct {
	email    string
	secret   string
	verified bool
}

func (v fakeVault) Owner() (string, string, bool) { return v.email, v.secret, v.verified }

type fakeInviter struct {
	last   InviteRequest
	invite Invite
	err    error
}

func (i *fakeInviter) Issue(_ context.Context, in InviteRequest) (Invite, error) {
	i.last = in
	if i.err != nil {
		return Invite{}, i.err
	}
	if i.invite.Token == "" {
		return Invite{ID: "invite-id", Token: "orbit-inv.token", Email: in.Email, ExpiresAt: time.Now().Add(in.TTL)}, nil
	}
	return i.invite, nil
}

type fakeMailer struct {
	sent int
	err  error
}

func (m *fakeMailer) SendInvite(context.Context, string, MailData) error {
	m.sent++
	return m.err
}

func newStaffService(dir Directory) *Service {
	return &Service{
		Directory: dir,
		Vault:     fakeVault{email: "admin@example.com", secret: "owner-secret", verified: true},
		Inviter:   &fakeInviter{},
		Mailer:    &fakeMailer{},
		LookupEnv: func(string) string { return "" },
	}
}

func TestCreateRequiresUIDAndForward(t *testing.T) {
	svc := newStaffService(&fakeDirectory{})

	if _, err := svc.Create(context.Background(), CreateInput{Forward: "a@example.com"}); clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("uid error = %v, want usage", err)
	}
	if _, err := svc.Create(context.Background(), CreateInput{UID: "alice"}); clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("forward error = %v, want usage", err)
	}
}

func TestCreatePassesGroupsAndIdempotency(t *testing.T) {
	dir := &fakeDirectory{}
	svc := newStaffService(dir)

	res, err := svc.Create(context.Background(), CreateInput{
		UID:            "alice",
		DisplayName:    "Alice",
		Forward:        "alice@example.com",
		Groups:         "dev, ops",
		IdempotencyKey: "fixed-key",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Member == nil || res.Member.UID != "alice" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if dir.created == nil {
		t.Fatal("expected a create call")
	}
	if got := strings.Join(dir.created.Groups, ","); got != "dev,ops" {
		t.Errorf("groups = %q", got)
	}
	if dir.created.IdempotencyKey != "fixed-key" {
		t.Errorf("idempotency key = %q", dir.created.IdempotencyKey)
	}
}

func TestCreateInviteDispatchesMail(t *testing.T) {
	inviter := &fakeInviter{}
	mailer := &fakeMailer{}
	svc := newStaffService(&fakeDirectory{})
	svc.Inviter = inviter
	svc.Mailer = mailer

	res, err := svc.Create(context.Background(), CreateInput{
		UID:         "alice",
		DisplayName: "Alice",
		Forward:     "alice@example.com",
		Invite:      true,
		InviteTTL:   "7d",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Invite == nil || res.Invite.Token == "" {
		t.Fatalf("expected an invite, got %+v", res)
	}
	if inviter.last.CreatedBy != "admin@example.com" {
		t.Errorf("createdBy = %q", inviter.last.CreatedBy)
	}
	if string(inviter.last.Secret) != "owner-secret" {
		t.Errorf("secret = %q, want the owner secret", inviter.last.Secret)
	}
	if mailer.sent != 1 || !res.Invite.Dispatched {
		t.Errorf("expected one dispatched mail, got %d", mailer.sent)
	}
}

func TestCreateInviteNoSend(t *testing.T) {
	mailer := &fakeMailer{}
	svc := newStaffService(&fakeDirectory{})
	svc.Mailer = mailer

	res, err := svc.Create(context.Background(), CreateInput{
		UID:     "alice",
		Forward: "alice@example.com",
		Invite:  true,
		NoSend:  true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if mailer.sent != 0 {
		t.Errorf("expected no mail, got %d", mailer.sent)
	}
	if res.Invite == nil || res.Invite.Dispatched {
		t.Errorf("unexpected invite: %+v", res.Invite)
	}
}

func TestCreateInviteFailureKeepsAccount(t *testing.T) {
	svc := newStaffService(&fakeDirectory{})
	svc.Inviter = &fakeInviter{err: errors.New("store unavailable")}

	res, err := svc.Create(context.Background(), CreateInput{UID: "alice", Forward: "alice@example.com", Invite: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Member == nil {
		t.Fatal("expected the account to be reported")
	}
	if !strings.Contains(res.InviteWarning, "invite generation failed") {
		t.Errorf("warning = %q", res.InviteWarning)
	}
}

func TestUpdateRejectsUnconfirmedProfileNames(t *testing.T) {
	dir := &fakeDirectory{updateRes: &domain.Member{UID: "alice"}}
	svc := newStaffService(dir)

	_, err := svc.Update(context.Background(), "alice", domain.UpdateInput{FirstName: "Alice"})
	if err == nil || !strings.Contains(err.Error(), "does not confirm profile-name updates") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateAcceptsConfirmedProfileNames(t *testing.T) {
	dir := &fakeDirectory{updateRes: &domain.Member{UID: "alice", FirstName: "Alice"}}
	svc := newStaffService(dir)

	member, err := svc.Update(context.Background(), "alice", domain.UpdateInput{FirstName: "Alice"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if member.FirstName != "Alice" {
		t.Errorf("member = %+v", member)
	}
}

func TestResetPasswordPassesTargets(t *testing.T) {
	dir := &fakeDirectory{}
	svc := newStaffService(dir)

	res, err := svc.ResetPassword(context.Background(), "alice", true, false)
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if res.LDAPPassword != "rotated" || !dir.resetLDAP || dir.resetMail {
		t.Errorf("unexpected reset: %+v ldap=%v mailbox=%v", res, dir.resetLDAP, dir.resetMail)
	}
}

func TestRecreateRefusesUnsafeAndValidUIDs(t *testing.T) {
	svc := newStaffService(&fakeDirectory{})

	if err := svc.Recreate("alice;echo unsafe"); err == nil || !strings.Contains(err.Error(), "provide a valid --uid") {
		t.Fatalf("unexpected error: %v", err)
	}
	err := svc.Recreate("alice")
	if err == nil || !strings.Contains(err.Error(), "staff recreate is disabled for alice") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "echo unsafe") {
		t.Fatalf("untrusted uid echoed: %v", err)
	}
}
