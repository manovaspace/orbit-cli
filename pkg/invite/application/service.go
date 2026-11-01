// Package application implements the invitation use cases over store, mailer
// and owner-vault ports.
package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/invite/domain"
)

// Store persists invitation records.
type Store interface {
	SaveInvite(*domain.InviteRecord) error
	ListInvites() ([]*domain.InviteRecord, error)
	GetInvite(tokenOrID string) (*domain.InviteRecord, error)
	RevokeInvite(tokenOrID string) (*domain.InviteRecord, error)
	RevokeAllInvites() ([]*domain.InviteRecord, error)
}

// Mailer delivers invitation email.
type Mailer interface {
	SendInvite(ctx context.Context, to string, data domain.EmailData) error
}

// OwnerVault exposes the sealed platform owner identity.
type OwnerVault interface {
	Owner() (email string, secret string, verified bool)
}

// UnverifiedOwner is the remediation message shown when no owner vault is sealed.
const UnverifiedOwner = "platform ownership is unverified; run 'orbit admin init --owner <email>' to verify ownership before issuing invitations"

// Service is the invitation use-case layer.
type Service struct {
	Store     Store
	Mailer    Mailer
	Owner     OwnerVault
	LookupEnv func(string) string
	Now       func() time.Time
	Out       io.Writer
	Err       io.Writer
	UI        UI
}

// UI carries the decoration cmd/orbit already renders.
type UI struct {
	Warn    string
	Warning func(string) string
}

func (s *Service) errOut() io.Writer {
	if s.Err == nil {
		return io.Discard
	}
	return s.Err
}

func (s *Service) env(key string) string {
	if s.LookupEnv == nil {
		return ""
	}
	return s.LookupEnv(key)
}

func (s *Service) warning(text string) string {
	if s.UI.Warning == nil {
		return text
	}
	return s.UI.Warning(text)
}

// IssueInput asks for a signed invitation without any human output.
type IssueInput struct {
	Email       string
	DisplayName string
	Scope       string
	TTL         time.Duration
	CreatedBy   string
	Secret      []byte
	Metadata    map[string]string
}

// IssueResult is a stored invitation.
type IssueResult struct {
	Record *domain.InviteRecord
	Claims *domain.InviteClaims
}

// Issue signs, stores and returns an invitation.
func (s *Service) Issue(_ context.Context, in IssueInput) (IssueResult, error) {
	scope := strings.TrimSpace(in.Scope)
	if scope == "" {
		scope = "core"
	}
	req := domain.InviteRequest{
		Email:       in.Email,
		DisplayName: strings.TrimSpace(in.DisplayName),
		Scope:       scope,
		TTL:         in.TTL,
		CreatedBy:   in.CreatedBy,
		Metadata:    in.Metadata,
	}

	token, claims, err := domain.GenerateToken(req, in.Secret)
	if err != nil {
		return IssueResult{}, clierr.Wrap(err, clierr.Failed("failed to generate invite token"))
	}

	rec := &domain.InviteRecord{
		ID:          claims.ID,
		Email:       claims.Email,
		DisplayName: claims.DisplayName,
		Scope:       claims.Scope,
		Token:       token,
		Revoked:     false,
		IssuedAt:    claims.IssuedAt,
		ExpiresAt:   claims.ExpiresAt,
		CreatedBy:   claims.CreatedBy,
		Metadata:    claims.Metadata,
	}
	if err := s.Store.SaveInvite(rec); err != nil {
		return IssueResult{}, clierr.Wrap(err, clierr.Failed("failed to save invitation record"))
	}
	return IssueResult{Record: rec, Claims: claims}, nil
}

// CreateInput is one `invite create` invocation.
type CreateInput struct {
	Email       string
	DisplayName string
	Scope       string
	Expires     string
	SecretFlag  string
	SecretEnv   string
	Insecure    bool
	Alias       bool
	TOTP        bool
	Send        bool
	Mailer      Mailer
	MailHost    string
	MailPort    string
}

// CreateResult is what `invite create` produced.
type CreateResult struct {
	Token          string               `json:"token"`
	Email          string               `json:"email"`
	DisplayName    string               `json:"displayName,omitempty"`
	Scope          string               `json:"scope"`
	CreatedBy      string               `json:"createdBy,omitempty"`
	InviteID       string               `json:"inviteId"`
	IssuedAt       time.Time            `json:"issuedAt"`
	ExpiresAt      time.Time            `json:"expiresAt"`
	TOTPSecret     string               `json:"totpSecret,omitempty"`
	TOTPURI        string               `json:"totpUri,omitempty"`
	FallbackSecret bool                 `json:"fallbackSecret"`
	MailDispatched bool                 `json:"mailDispatched"`
	MailError      string               `json:"mailError,omitempty"`
	Record         *domain.InviteRecord `json:"-"`
}

// Create validates the request, signs an invitation, stores it and optionally
// dispatches the invitation email.
func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	email := strings.TrimSpace(in.Email)
	if email == "" {
		return CreateResult{}, clierr.Usage(`required flag(s) "email" not set (use -i for interactive mode)`)
	}
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return CreateResult{}, clierr.Usage(fmt.Sprintf("invalid email address %q", email))
	}

	ttl, err := domain.ParseTTL(in.Expires)
	if err != nil {
		return CreateResult{}, clierr.Usage(err.Error())
	}

	ownerVerified := false
	ownerSecret := ""
	ownerEmail := ""
	if s.Owner != nil {
		ownerEmail, ownerSecret, ownerVerified = s.Owner.Owner()
	}

	isBypassed := in.Insecure || s.env("ORBIT_INSECURE_SKIP_OWNER_CHECK") == "true"
	if !ownerVerified && !isBypassed {
		return CreateResult{}, clierr.Auth(UnverifiedOwner)
	}

	verifiedSecret := ""
	if ownerVerified {
		verifiedSecret = ownerSecret
	}

	secret, isFallback := domain.ResolveSecret(s.env, in.SecretFlag, in.SecretEnv, verifiedSecret)
	if len(secret) == 0 {
		return CreateResult{}, clierr.Usage("signing secret cannot be empty")
	}
	if isFallback {
		fmt.Fprintf(s.errOut(), "%s %s\n\n",
			s.UI.Warn,
			s.warning("Using default dev signing secret. For production tokens, set $MANOVA_INVITE_SECRET or pass --secret."),
		)
	}

	var metadata map[string]string
	if in.Alias {
		metadata = map[string]string{"create_alias": "true"}
	}

	var totpSecret, totpURI string
	if in.TOTP {
		secretVal, uriVal, err := domain.GenerateTOTP("Orbit", email)
		if err != nil {
			return CreateResult{}, clierr.Wrap(err, clierr.Failed("failed to generate TOTP secret"))
		}
		totpSecret, totpURI = secretVal, uriVal
		if metadata == nil {
			metadata = make(map[string]string)
		}
		metadata["otpauth"] = uriVal
	}

	createdBy := ""
	if ownerVerified {
		createdBy = ownerEmail
	}

	issued, err := s.Issue(ctx, IssueInput{
		Email:       email,
		DisplayName: in.DisplayName,
		Scope:       in.Scope,
		TTL:         ttl,
		CreatedBy:   createdBy,
		Secret:      secret,
		Metadata:    metadata,
	})
	if err != nil {
		return CreateResult{}, err
	}
	claims := issued.Claims

	result := CreateResult{
		Token:          issued.Record.Token,
		Email:          claims.Email,
		DisplayName:    claims.DisplayName,
		Scope:          claims.Scope,
		CreatedBy:      claims.CreatedBy,
		InviteID:       claims.ID,
		IssuedAt:       claims.IssuedAt,
		ExpiresAt:      claims.ExpiresAt,
		TOTPSecret:     totpSecret,
		TOTPURI:        totpURI,
		FallbackSecret: isFallback,
		Record:         issued.Record,
	}

	if in.Send && in.Mailer != nil {
		data := domain.EmailData{
			RecipientName:  claims.DisplayName,
			RecipientEmail: claims.Email,
			Token:          issued.Record.Token,
			ShortCode:      issued.Record.Token,
			Scope:          claims.Scope,
			ExpiresAt:      claims.ExpiresAt,
			ExpiresInHuman: domain.FormatRemaining(time.Until(claims.ExpiresAt)),
			CLICommand:     fmt.Sprintf("orbit onboard --token %s", issued.Record.Token),
			CurlCommand:    fmt.Sprintf("curl -fsSL https://orbit.manova.space | bash -s -- onboard --token %s", issued.Record.Token),
		}
		if err := in.Mailer.SendInvite(ctx, claims.Email, data); err != nil {
			result.MailError = err.Error()
		} else {
			result.MailDispatched = true
		}
	}

	return result, nil
}

// ListResult is the invitation listing.
type ListResult struct {
	All       []*domain.InviteRecord `json:"all"`
	Displayed []*domain.InviteRecord `json:"displayed"`
	Total     int                    `json:"total"`
	Active    int                    `json:"active"`
	Revoked   int                    `json:"revoked"`
	Expired   int                    `json:"expired"`
}

// List returns the stored invitations, filtered to active ones unless all is set.
func (s *Service) List(all bool) (ListResult, error) {
	records, err := s.Store.ListInvites()
	if err != nil {
		return ListResult{}, clierr.Wrap(err, clierr.Failed("failed to list invitations"))
	}

	result := ListResult{All: records, Displayed: []*domain.InviteRecord{}, Total: len(records)}
	for _, rec := range records {
		switch rec.Status() {
		case "active":
			result.Active++
		case "revoked":
			result.Revoked++
		case "expired":
			result.Expired++
		}
		if all || rec.Status() == "active" {
			result.Displayed = append(result.Displayed, rec)
		}
	}
	return result, nil
}

// RevokeResult is what `invite revoke` produced.
type RevokeResult struct {
	Revoked []*domain.InviteRecord `json:"revoked"`
	None    bool                   `json:"none"`
}

// Revoke marks one invitation, or every active invitation, as revoked.
func (s *Service) Revoke(tokenOrID string, all bool) (RevokeResult, error) {
	if all {
		if len(strings.TrimSpace(tokenOrID)) > 0 {
			return RevokeResult{}, clierr.Usage("cannot specify positional invite token/ID when --all is used")
		}
		revoked, err := s.Store.RevokeAllInvites()
		if err != nil {
			return RevokeResult{}, clierr.Wrap(err, clierr.Failed("failed to revoke all invitations"))
		}
		if len(revoked) == 0 {
			return RevokeResult{None: true}, nil
		}
		return RevokeResult{Revoked: revoked}, nil
	}

	value := strings.TrimSpace(tokenOrID)
	if value == "" {
		return RevokeResult{}, clierr.Usage("invite token or ID required (or use --all to revoke all active invitations)")
	}
	rec, err := s.Store.RevokeInvite(value)
	if err != nil {
		if errors.Is(err, domain.ErrInviteNotFound) {
			return RevokeResult{}, clierr.Failed(fmt.Sprintf("invitation %q not found in store", value))
		}
		return RevokeResult{}, clierr.Wrap(err, clierr.Failed("failed to revoke invitation"))
	}
	return RevokeResult{Revoked: []*domain.InviteRecord{rec}}, nil
}
