package invite

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Mailer defines the interface for delivering invitation and challenge emails.
type Mailer interface {
	SendInvite(ctx context.Context, to string, data EmailData) error
	SendOwnerChallenge(ctx context.Context, to string, data OwnerChallengeEmailData) error
}

// MailerConfig holds SMTP configuration parameters.
type MailerConfig struct {
	Host               string
	Port               string
	User               string
	Pass               string
	From               string
	InsecureSkipVerify bool
}

// SMTPMailer delivers multipart invitation and challenge emails over SMTP.
type SMTPMailer struct {
	cfg MailerConfig
}

// NewSMTPMailer constructs a new SMTPMailer with the specified configuration.
// Defaults Host to "mail.manova.space", Port to "587", and From to "Orbit Platform <noreply@manova.space>".
func NewSMTPMailer(cfg MailerConfig) *SMTPMailer {
	if cfg.Host == "" {
		cfg.Host = "mail.manova.space"
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	if cfg.From == "" {
		cfg.From = "Orbit Platform <noreply@manova.space>"
	}
	return &SMTPMailer{cfg: cfg}
}

// NewMailerFromEnv initializes an SMTPMailer from environment variables.
func NewMailerFromEnv() *SMTPMailer {
	host := getEnvFallback("ORBIT_SMTP_HOST", "SMTP_HOST", "mail.manova.space")
	port := getEnvFallback("ORBIT_SMTP_PORT", "SMTP_PORT", "587")
	user := getEnvFallback("ORBIT_SMTP_USER", "SMTP_USER", "")
	pass := getEnvFallback("ORBIT_SMTP_PASS", "SMTP_PASS", "")
	from := getEnvFallback("ORBIT_SMTP_FROM", "SMTP_FROM", "Orbit Platform <noreply@manova.space>")

	return NewSMTPMailer(MailerConfig{
		Host: host,
		Port: port,
		User: user,
		Pass: pass,
		From: from,
	})
}

// SendInvite renders the invitation email and dispatches it as a multipart/alternative MIME message.
func (m *SMTPMailer) SendInvite(ctx context.Context, to string, data EmailData) error {
	subject, textBody, htmlBody, err := RenderInviteEmail(data)
	if err != nil {
		return fmt.Errorf("failed to render invite email: %w", err)
	}
	return m.sendMultipartEmail(ctx, to, subject, textBody, htmlBody)
}

// SendOwnerChallenge renders the owner verification OTP email and dispatches it as a multipart/alternative MIME message.
func (m *SMTPMailer) SendOwnerChallenge(ctx context.Context, to string, data OwnerChallengeEmailData) error {
	subject, textBody, htmlBody, err := RenderOwnerChallengeEmail(data)
	if err != nil {
		return fmt.Errorf("failed to render owner challenge email: %w", err)
	}
	return m.sendMultipartEmail(ctx, to, subject, textBody, htmlBody)
}

// sendMultipartEmail dispatches a multipart/alternative MIME email over SMTP with STARTTLS or implicit TLS.
func (m *SMTPMailer) sendMultipartEmail(ctx context.Context, to, subject, textBody, htmlBody string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("recipient email cannot be empty")
	}

	boundary := generateBoundary()
	var msg strings.Builder

	// MIME Headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", m.cfg.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z)))
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// Plaintext Part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	msg.WriteString(textBody)
	msg.WriteString("\r\n\r\n")

	// HTML Part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n\r\n")

	// Closing Boundary
	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	addr := net.JoinHostPort(m.cfg.Host, m.cfg.Port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConfig := &tls.Config{
		ServerName:         m.cfg.Host,
		InsecureSkipVerify: m.cfg.InsecureSkipVerify,
	}

	var conn net.Conn
	var err error

	if m.cfg.Port == "465" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTPS server %s: %w", addr, err)
		}
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTP server %s: %w", addr, err)
		}
	}

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to initialize SMTP client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if m.cfg.Port != "465" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS failed on %s: %w", addr, err)
			}
		}
	}

	if m.cfg.User != "" && m.cfg.Pass != "" {
		auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Pass, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	fromAddr := extractEmailAddress(m.cfg.From)
	if err := client.Mail(fromAddr); err != nil {
		return fmt.Errorf("SMTP MAIL command failed: %w", err)
	}

	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT command failed for %s: %w", to, err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}

	if _, err := w.Write([]byte(msg.String())); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to write email message payload: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize email message data: %w", err)
	}

	return client.Quit()
}

func generateBoundary() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "----=_NextPart_" + hex.EncodeToString(b)
}

func extractEmailAddress(from string) string {
	if start := strings.Index(from, "<"); start != -1 {
		if end := strings.Index(from[start:], ">"); end != -1 {
			return strings.TrimSpace(from[start+1 : start+end])
		}
	}
	return strings.TrimSpace(from)
}

func getEnvFallback(keys ...string) string {
	for i := 0; i < len(keys)-1; i++ {
		if val := os.Getenv(keys[i]); val != "" {
			return val
		}
	}
	return keys[len(keys)-1]
}

