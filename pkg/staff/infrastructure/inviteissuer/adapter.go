// Package inviteissuer adapts the invitation service to the staff ports.
package inviteissuer

import (
	"context"
	"fmt"
	"time"

	inviteapp "github.com/manovaspace/orbit-cli/pkg/invite/application"
	invitedomain "github.com/manovaspace/orbit-cli/pkg/invite/domain"
	staffapp "github.com/manovaspace/orbit-cli/pkg/staff/application"
)

// Issuer signs and stores onboarding tokens through the invitation service.
type Issuer struct {
	Service *inviteapp.Service
}

// Issue returns a stored onboarding token.
func (i *Issuer) Issue(ctx context.Context, in staffapp.InviteRequest) (staffapp.Invite, error) {
	res, err := i.Service.Issue(ctx, inviteapp.IssueInput{
		Email:       in.Email,
		DisplayName: in.DisplayName,
		Scope:       "core",
		TTL:         in.TTL,
		CreatedBy:   in.CreatedBy,
		Secret:      in.Secret,
	})
	if err != nil {
		return staffapp.Invite{}, err
	}
	return staffapp.Invite{
		ID:        res.Record.ID,
		Token:     res.Record.Token,
		Email:     res.Record.Email,
		ExpiresAt: res.Record.ExpiresAt,
	}, nil
}

// Mailer delivers onboarding mail through the SMTP adapter.
type Mailer struct {
	Sender interface {
		SendInvite(ctx context.Context, to string, data invitedomain.EmailData) error
	}
}

// SendInvite renders and sends the onboarding email.
func (m *Mailer) SendInvite(ctx context.Context, to string, data staffapp.MailData) error {
	ttl := time.Until(data.ExpiresAt)
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return m.Sender.SendInvite(ctx, to, invitedomain.EmailData{
		RecipientName:  data.RecipientName,
		RecipientEmail: data.RecipientEmail,
		Token:          data.Token,
		ShortCode:      data.Token,
		Scope:          data.Scope,
		ExpiresAt:      data.ExpiresAt,
		ExpiresInHuman: invitedomain.FormatRemaining(ttl),
		CLICommand:     fmt.Sprintf("orbit onboard --token %s", data.Token),
		CurlCommand:    fmt.Sprintf("curl -fsSL https://orbit.manova.space | bash -s -- onboard --token %s", data.Token),
	})
}
