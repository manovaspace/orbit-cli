// Package smtp adapts the shared SMTP mailer to the owner mail port.
package smtp

import (
	"context"

	invitedomain "github.com/manovaspace/orbit-cli/pkg/invite/domain"
	invitesmtp "github.com/manovaspace/orbit-cli/pkg/invite/infrastructure/smtp"
	ownerapp "github.com/manovaspace/orbit-cli/pkg/owner/application"
)

// Mailer sends owner challenge email through the shared SMTP adapter.
type Mailer struct {
	Sender *invitesmtp.SMTPMailer
}

// New builds a Mailer from the shared SMTP configuration.
func New(cfg ownerapp.SMTPConfig) *Mailer {
	return &Mailer{Sender: invitesmtp.NewSMTPMailer(invitesmtp.MailerConfig{
		Host: cfg.Host,
		Port: cfg.Port,
		User: cfg.User,
		Pass: cfg.Pass,
		From: cfg.From,
	})}
}

// SendOwnerChallenge renders and sends the challenge email.
func (m *Mailer) SendOwnerChallenge(ctx context.Context, to string, data ownerapp.ChallengeEmail) error {
	return m.Sender.SendOwnerChallenge(ctx, to, invitedomain.OwnerChallengeEmailData{
		OwnerEmail:  data.OwnerEmail,
		OTPCode:     data.OTPCode,
		ExpiresIn:   data.ExpiresIn,
		ServerHost:  data.ServerHost,
		GeneratedAt: data.GeneratedAt,
	})
}
