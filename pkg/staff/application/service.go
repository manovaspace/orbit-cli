// Package application implements the staff use cases over the directory,
// owner-vault, inviter and mailer ports.
package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	invitedomain "github.com/manovaspace/orbit-cli/pkg/invite/domain"
	"github.com/manovaspace/orbit-cli/pkg/staff/domain"
)

// UnverifiedOwner is the remediation message shown when no owner vault is sealed.
const UnverifiedOwner = "platform ownership is unverified; run 'orbit admin init --owner <email>' to verify ownership before managing staff"

// Directory is the staff control plane.
type Directory interface {
	Create(ctx context.Context, in domain.CreateInput) (*domain.CreateResult, error)
	List(ctx context.Context) ([]domain.Member, error)
	Get(ctx context.Context, uid string) (*domain.Member, error)
	Update(ctx context.Context, uid string, in domain.UpdateInput) (*domain.Member, error)
	Disable(ctx context.Context, uid string) error
	Enable(ctx context.Context, uid string) error
	Delete(ctx context.Context, uid string) error
	ResetPassword(ctx context.Context, uid string, ldap, mailbox bool) (*domain.ResetResult, error)
}

// Vault exposes the sealed platform owner identity.
type Vault interface {
	Owner() (email string, secret string, verified bool)
}

// InviteRequest asks for an onboarding token.
type InviteRequest struct {
	Email       string
	DisplayName string
	TTL         time.Duration
	Secret      []byte
	CreatedBy   string
}

// Invite is a stored onboarding token.
type Invite struct {
	ID        string
	Token     string
	Email     string
	ExpiresAt time.Time
}

// Inviter signs and stores onboarding tokens.
type Inviter interface {
	Issue(ctx context.Context, in InviteRequest) (Invite, error)
}

// MailData is the onboarding mail payload.
type MailData struct {
	RecipientName  string
	RecipientEmail string
	Token          string
	Scope          string
	ExpiresAt      time.Time
}

// Mailer delivers onboarding mail.
type Mailer interface {
	SendInvite(ctx context.Context, to string, data MailData) error
}

// Service is the staff use-case layer.
type Service struct {
	Directory Directory
	Vault     Vault
	Inviter   Inviter
	Mailer    Mailer
	LookupEnv func(string) string
	Now       func() time.Time
}

func (s *Service) env(key string) string {
	if s.LookupEnv == nil {
		return ""
	}
	return s.LookupEnv(key)
}

// CreateInput is one `staff create` invocation.
type CreateInput struct {
	UID            string
	DisplayName    string
	FirstName      string
	LastName       string
	Forward        string
	Groups         string
	IdempotencyKey string
	Invite         bool
	InviteEmail    string
	InviteTTL      string
	NoSend         bool
}

// CreateResult is what `staff create` produced.
type CreateResult struct {
	Member *domain.CreateResult `json:"member"`
	Invite *domain.Invite       `json:"invite,omitempty"`
	// InviteWarning is set when invite generation failed but the account exists.
	InviteWarning string `json:"inviteWarning,omitempty"`
}

// Create makes a staff member and optionally issues an onboarding invitation.
func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	uid := strings.TrimSpace(in.UID)
	if uid == "" {
		return CreateResult{}, clierr.Usage("required flag --uid")
	}
	forward := strings.TrimSpace(in.Forward)
	if forward == "" || !strings.Contains(forward, "@") {
		return CreateResult{}, clierr.Usage("required flag --forward with an email address")
	}

	idem := strings.TrimSpace(in.IdempotencyKey)
	if idem == "" {
		var err error
		idem, err = domain.NewIdempotencyKey()
		if err != nil {
			return CreateResult{}, clierr.Wrap(err, clierr.Failed("failed to generate idempotency key"))
		}
	}

	created, err := s.Directory.Create(ctx, domain.CreateInput{
		UID:             uid,
		DisplayName:     strings.TrimSpace(in.DisplayName),
		FirstName:       strings.TrimSpace(in.FirstName),
		LastName:        strings.TrimSpace(in.LastName),
		PersonalForward: forward,
		Groups:          domain.SplitGroups(in.Groups),
		IdempotencyKey:  idem,
	})
	if err != nil {
		return CreateResult{}, err
	}

	result := CreateResult{Member: created}
	if !in.Invite {
		return result, nil
	}

	invite, warning := s.issueInvite(ctx, in, forward)
	result.Invite = invite
	result.InviteWarning = warning
	return result, nil
}

func (s *Service) issueInvite(ctx context.Context, in CreateInput, forward string) (*domain.Invite, string) {
	inviteEmail := strings.TrimSpace(in.InviteEmail)
	if inviteEmail == "" {
		inviteEmail = forward
	}

	ttl, err := invitedomain.ParseTTL(in.InviteTTL)
	if err != nil {
		return nil, fmt.Sprintf("invite generation failed: invalid --invite-ttl: %v", err)
	}

	createdBy := ""
	ownerSecret := ""
	if s.Vault != nil {
		email, secret, verified := s.Vault.Owner()
		if verified {
			createdBy = email
			ownerSecret = secret
		}
	}

	if s.Inviter == nil {
		return nil, "invite generation failed: no inviter configured"
	}

	secret := invitedomain.ResolveStaffSecret(s.env, ownerSecret)
	stored, err := s.Inviter.Issue(ctx, InviteRequest{
		Email:       inviteEmail,
		DisplayName: strings.TrimSpace(in.DisplayName),
		TTL:         ttl,
		Secret:      secret,
		CreatedBy:   createdBy,
	})
	if err != nil {
		return nil, fmt.Sprintf("invite generation failed: %v", err)
	}

	invite := &domain.Invite{ID: stored.ID, Token: stored.Token, Email: stored.Email, ExpiresAt: stored.ExpiresAt}
	if in.NoSend || s.Mailer == nil {
		return invite, ""
	}

	data := MailData{
		RecipientName:  strings.TrimSpace(in.DisplayName),
		RecipientEmail: inviteEmail,
		Token:          stored.Token,
		Scope:          "core",
		ExpiresAt:      stored.ExpiresAt,
	}
	if err := s.Mailer.SendInvite(ctx, inviteEmail, data); err != nil {
		invite.DispatchErr = err.Error()
		return invite, ""
	}
	invite.Dispatched = true
	return invite, ""
}

// List returns every staff member.
func (s *Service) List(ctx context.Context) ([]domain.Member, error) {
	members, err := s.Directory.List(ctx)
	if err != nil {
		return nil, err
	}
	return members, nil
}

// Get returns one staff member.
func (s *Service) Get(ctx context.Context, uid string) (*domain.Member, error) {
	return s.Directory.Get(ctx, uid)
}

// Update changes a staff member and rejects servers that ignore profile names.
func (s *Service) Update(ctx context.Context, uid string, in domain.UpdateInput) (*domain.Member, error) {
	member, err := s.Directory.Update(ctx, uid, in)
	if err != nil {
		return nil, err
	}
	if (in.FirstName != "" && member.FirstName != in.FirstName) || (in.LastName != "" && member.LastName != in.LastName) {
		return nil, clierr.Failed("staff server does not confirm profile-name updates; update orbit-staff before retrying")
	}
	return member, nil
}

// Disable deactivates a staff member.
func (s *Service) Disable(ctx context.Context, uid string) error {
	return s.Directory.Disable(ctx, uid)
}

// Enable reactivates a staff member.
func (s *Service) Enable(ctx context.Context, uid string) error {
	return s.Directory.Enable(ctx, uid)
}

// Delete removes a staff member's LLDAP identity.
func (s *Service) Delete(ctx context.Context, uid string) error {
	return s.Directory.Delete(ctx, uid)
}

// ResetPassword rotates the shared LLDAP password.
func (s *Service) ResetPassword(ctx context.Context, uid string, ldap, mailbox bool) (*domain.ResetResult, error) {
	return s.Directory.ResetPassword(ctx, uid, ldap, mailbox)
}

var staffUIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// Recreate refuses the legacy destructive path and explains the safe sequence.
func (s *Service) Recreate(uid string) error {
	uid = strings.TrimSpace(uid)
	if !staffUIDPattern.MatchString(uid) {
		return clierr.Failed("staff recreate is disabled; provide a valid --uid to display the safe Keycloak cleanup sequence")
	}
	return clierr.Failed(fmt.Sprintf("staff recreate is disabled for %s because it can leave a stale Keycloak login. Before deleting any LLDAP identity, deploy /opt/orbit-infra/scripts/remove-keycloak-staff-user.py on the identity host and require its --preflight check for this UID to succeed; stop if it is unavailable or preflight fails. Then follow the staff lifecycle guide for a reviewed dry run, confirmed Keycloak cleanup, and staff creation only after cleanup succeeds. No identity was changed", uid))
}
