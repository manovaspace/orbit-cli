// Package application implements the owner, invite and staff use cases that
// back the admin command group, reaching the outside world only through ports.
package application

import (
	"context"
	"io"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner/domain"
)

// Vault is the sealed owner record on disk.
type Vault interface {
	Load() (*domain.OwnerRecord, error)
	Save(*domain.OwnerRecord) error
	CheckPermissions() error
	Exists() bool
	FilePath() string
}

// Server is the Orbit auth service that dispatches challenges and registers grants.
type Server interface {
	InitiateChallenge(ctx context.Context, email string) (domain.ChallengeDispatch, error)
	VerifyChallenge(ctx context.Context, email, code string) (domain.Verification, error)
	CreateGrant(ctx context.Context, email, role, code, masterSecret string, ttl time.Duration) (domain.GrantRegistration, error)
}

// Mailer delivers owner challenge email.
type Mailer interface {
	SendOwnerChallenge(ctx context.Context, to string, data ChallengeEmail) error
}

// ChallengeEmail mirrors the templates package without leaking it into this layer.
type ChallengeEmail struct {
	OwnerEmail  string
	OTPCode     string
	ExpiresIn   string
	ServerHost  string
	GeneratedAt time.Time
}

// SMTPConfig is the mail transport configuration resolved from flags and environment.
type SMTPConfig struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

// Telegram dispatches an admin grant to the operations forum.
type Telegram interface {
	SendGrant(ctx context.Context, recipient, code, role string, expiresAt time.Time) error
}

// Prompts asks the operator for input the flags left empty.
type Prompts struct {
	Email         func() string
	Code          func() string
	ConfirmRotate func(email string) bool
}

// UI carries the decoration cmd/orbit already renders so this layer never
// imports a styling library.
type UI struct {
	Arrow, OK, Warn, Info, Error string
	Title, Header, Bold          func(string) string
	Subtle, Code, Success        func(string) string
	Warning, ErrorStyle          func(string) string
	Card                         func(title, content string) string
}

func (u UI) render(f func(string) string, text string) string {
	if f == nil {
		return text
	}
	return f(text)
}

// Service is the owner/admin use-case layer.
type Service struct {
	Vault         Vault
	Server        Server
	Telegram      Telegram
	MailerFactory func(SMTPConfig) Mailer
	LookupEnv     func(string) string
	Now           func() time.Time
	Out           io.Writer
	Err           io.Writer
	UI            UI
	Prompts       Prompts
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) errOut() io.Writer {
	if s.Err == nil {
		return io.Discard
	}
	return s.Err
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now()
}

func (s *Service) env(key string) string {
	if s.LookupEnv == nil {
		return ""
	}
	return s.LookupEnv(key)
}

func (s *Service) icon(glyph string) string {
	if glyph == "" {
		return ""
	}
	return glyph + " "
}

func (s *Service) bold(text string) string      { return s.UI.render(s.UI.Bold, text) }
func (s *Service) header(text string) string    { return s.UI.render(s.UI.Header, text) }
func (s *Service) subtle(text string) string    { return s.UI.render(s.UI.Subtle, text) }
func (s *Service) code(text string) string      { return s.UI.render(s.UI.Code, text) }
func (s *Service) success(text string) string   { return s.UI.render(s.UI.Success, text) }
func (s *Service) warning(text string) string   { return s.UI.render(s.UI.Warning, text) }
func (s *Service) errorText(text string) string { return s.UI.render(s.UI.ErrorStyle, text) }
func (s *Service) title(text string) string     { return s.UI.render(s.UI.Title, text) }

func (s *Service) card(title, content string) string {
	if s.UI.Card == nil {
		return content
	}
	return s.UI.Card(title, content)
}

func (s *Service) promptEmail() string {
	if s.Prompts.Email == nil {
		return ""
	}
	return s.Prompts.Email()
}

func (s *Service) promptCode() string {
	if s.Prompts.Code == nil {
		return ""
	}
	return s.Prompts.Code()
}
