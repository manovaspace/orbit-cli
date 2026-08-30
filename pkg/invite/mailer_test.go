package invite

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// startMockSMTPServer starts a minimal in-memory SMTP server for testing.
func startMockSMTPServer(t *testing.T) (string, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on local port: %v", err)
	}

	receivedData := make([]string, 0)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := bufio.NewWriter(c)

				// Initial greeting
				_, _ = w.WriteString("220 mock.smtp.orbit Service Ready\r\n")
				_ = w.Flush()

				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					fields := strings.Fields(line)
					if len(fields) == 0 {
						continue
					}
					cmd := strings.ToUpper(fields[0])

					switch cmd {
					case "HELO", "EHLO":
						_, _ = w.WriteString("250-mock.smtp.orbit\r\n250 HELP\r\n")
						_ = w.Flush()
					case "MAIL":
						_, _ = w.WriteString("250 OK\r\n")
						_ = w.Flush()
					case "RCPT":
						_, _ = w.WriteString("250 OK\r\n")
						_ = w.Flush()
					case "DATA":
						_, _ = w.WriteString("354 Start mail input; end with <CRLF>.<CRLF>\r\n")
						_ = w.Flush()

						var bodyLines []string
						for {
							dataLine, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if dataLine == ".\r\n" || dataLine == ".\n" {
								break
							}
							bodyLines = append(bodyLines, dataLine)
						}
						receivedData = append(receivedData, strings.Join(bodyLines, ""))
						_, _ = w.WriteString("250 OK: queued\r\n")
						_ = w.Flush()
					case "QUIT":
						_, _ = w.WriteString("221 Bye\r\n")
						_ = w.Flush()
						return
					default:
						_, _ = w.WriteString("500 Command unrecognized\r\n")
						_ = w.Flush()
					}
				}
			}(conn)
		}
	}()

	addr := l.Addr().String()
	cleanup := func() {
		_ = l.Close()
		<-done
	}

	return addr, cleanup
}

func generateTestCertificate() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Orbit Test"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost", "127.0.0.1"},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}, nil
}

func startMockSTARTTLSServer(t *testing.T) (string, func()) {
	cert, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate self-signed cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on local port: %v", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := bufio.NewWriter(c)

				// Greeting
				_, _ = w.WriteString("220 mock.smtp.orbit Service Ready\r\n")
				_ = w.Flush()

				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					fields := strings.Fields(line)
					if len(fields) == 0 {
						continue
					}
					cmd := strings.ToUpper(fields[0])

					switch cmd {
					case "HELO", "EHLO":
						_, _ = w.WriteString("250-mock.smtp.orbit\r\n250-STARTTLS\r\n250 HELP\r\n")
						_ = w.Flush()
					case "STARTTLS":
						_, _ = w.WriteString("220 2.0.0 Ready to start TLS\r\n")
						_ = w.Flush()

						tlsConn := tls.Server(c, tlsConfig)
						if err := tlsConn.Handshake(); err != nil {
							return
						}
						c = tlsConn
						r = bufio.NewReader(tlsConn)
						w = bufio.NewWriter(tlsConn)
					case "MAIL":
						_, _ = w.WriteString("250 OK\r\n")
						_ = w.Flush()
					case "RCPT":
						_, _ = w.WriteString("250 OK\r\n")
						_ = w.Flush()
					case "DATA":
						_, _ = w.WriteString("354 Start mail input; end with <CRLF>.<CRLF>\r\n")
						_ = w.Flush()

						for {
							dataLine, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if dataLine == ".\r\n" || dataLine == ".\n" {
								break
							}
						}
						_, _ = w.WriteString("250 OK: queued\r\n")
						_ = w.Flush()
					case "QUIT":
						_, _ = w.WriteString("221 Bye\r\n")
						_ = w.Flush()
						return
					default:
						_, _ = w.WriteString("500 Command unrecognized\r\n")
						_ = w.Flush()
					}
				}
			}(conn)
		}
	}()

	addr := l.Addr().String()
	cleanup := func() {
		_ = l.Close()
		<-done
	}

	return addr, cleanup
}

func TestSMTPMailer_SendInvite(t *testing.T) {
	addr, cleanup := startMockSMTPServer(t)
	defer cleanup()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}

	mailer := NewSMTPMailer(MailerConfig{
		Host: host,
		Port: port,
		From: "Orbit <noreply@manova.space>",
	})

	data := EmailData{
		RecipientName:  "Test Dev",
		RecipientEmail: "dev@manova.space",
		Token:          "manova-inv.test.sig",
		ShortCode:      "999-111",
		ExpiresAt:      time.Now().Add(48 * time.Hour),
		ExpiresInHuman: "48 hours",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mailer.SendInvite(ctx, "dev@manova.space", data); err != nil {
		t.Fatalf("SendInvite failed: %v", err)
	}
}

func TestSMTPMailer_SendOwnerChallenge(t *testing.T) {
	addr, cleanup := startMockSMTPServer(t)
	defer cleanup()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}

	mailer := NewSMTPMailer(MailerConfig{
		Host: host,
		Port: port,
		From: "Orbit Platform <noreply@manova.space>",
	})

	data := OwnerChallengeEmailData{
		OwnerEmail:  "alirezaopmc@gmail.com",
		OTPCode:     "654321",
		ExpiresIn:   "10 minutes",
		ServerHost:  "mail.manova.space",
		GeneratedAt: time.Now().UTC(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mailer.SendOwnerChallenge(ctx, "alirezaopmc@gmail.com", data); err != nil {
		t.Fatalf("SendOwnerChallenge failed: %v", err)
	}
}

func TestSMTPMailer_STARTTLS(t *testing.T) {
	addr, cleanup := startMockSTARTTLSServer(t)
	defer cleanup()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}

	mailer := NewSMTPMailer(MailerConfig{
		Host:               host,
		Port:               port,
		From:               "Orbit Platform <noreply@manova.space>",
		InsecureSkipVerify: true,
	})

	data := OwnerChallengeEmailData{
		OwnerEmail:  "owner@manova.space",
		OTPCode:     "888999",
		ExpiresIn:   "10 minutes",
		ServerHost:  host,
		GeneratedAt: time.Now().UTC(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mailer.SendOwnerChallenge(ctx, "owner@manova.space", data); err != nil {
		t.Fatalf("SendOwnerChallenge with STARTTLS failed: %v", err)
	}
}

func TestNewSMTPMailer_Defaults(t *testing.T) {
	mailer := NewSMTPMailer(MailerConfig{})
	if mailer.cfg.Host != "mail.manova.space" {
		t.Errorf("expected default host mail.manova.space, got %s", mailer.cfg.Host)
	}
	if mailer.cfg.Port != "587" {
		t.Errorf("expected default port 587, got %s", mailer.cfg.Port)
	}
	if mailer.cfg.From != "Orbit Platform <noreply@manova.space>" {
		t.Errorf("expected default from 'Orbit Platform <noreply@manova.space>', got %s", mailer.cfg.From)
	}
}

func TestNewMailerFromEnv(t *testing.T) {
	t.Setenv("ORBIT_SMTP_HOST", "mail.example.com")
	t.Setenv("ORBIT_SMTP_PORT", "2525")
	t.Setenv("ORBIT_SMTP_FROM", "custom@manova.space")

	mailer := NewMailerFromEnv()
	if mailer.cfg.Host != "mail.example.com" {
		t.Errorf("expected host mail.example.com, got %s", mailer.cfg.Host)
	}
	if mailer.cfg.Port != "2525" {
		t.Errorf("expected port 2525, got %s", mailer.cfg.Port)
	}
	if mailer.cfg.From != "custom@manova.space" {
		t.Errorf("expected from custom@manova.space, got %s", mailer.cfg.From)
	}
}

func TestNewMailerFromEnv_Defaults(t *testing.T) {
	t.Setenv("ORBIT_SMTP_HOST", "")
	t.Setenv("ORBIT_SMTP_PORT", "")
	t.Setenv("ORBIT_SMTP_USER", "")
	t.Setenv("ORBIT_SMTP_PASS", "")
	t.Setenv("ORBIT_SMTP_FROM", "")

	mailer := NewMailerFromEnv()
	if mailer.cfg.Host != "mail.manova.space" {
		t.Errorf("expected host mail.manova.space, got %s", mailer.cfg.Host)
	}
	if mailer.cfg.Port != "587" {
		t.Errorf("expected port 587, got %s", mailer.cfg.Port)
	}
	if mailer.cfg.From != "Orbit Platform <noreply@manova.space>" {
		t.Errorf("expected from 'Orbit Platform <noreply@manova.space>', got %s", mailer.cfg.From)
	}
}

func TestSMTPMailer_SendOwnerChallenge_EmptyRecipient(t *testing.T) {
	mailer := NewSMTPMailer(MailerConfig{})
	data := OwnerChallengeEmailData{
		OTPCode: "123456",
	}
	err := mailer.SendOwnerChallenge(context.Background(), "", data)
	if err == nil {
		t.Fatal("expected error for empty recipient email, got nil")
	}
}

