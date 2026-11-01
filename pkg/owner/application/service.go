package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/owner/domain"
)

// InitInput is one `admin init` invocation.
type InitInput struct {
	Email       string
	DisplayName string
	Code        string
	ServerURL   string
	Force       bool
	NoSend      bool
}

// InitResult is what `admin init` produced.
type InitResult struct {
	AlreadyVerified bool   `json:"alreadyVerified"`
	Verified        bool   `json:"verified"`
	Email           string `json:"email"`
	DisplayName     string `json:"displayName,omitempty"`
	VerifiedAt      string `json:"verifiedAt,omitempty"`
	KeyFingerprint  string `json:"keyFingerprint,omitempty"`
	VaultLocation   string `json:"vaultLocation"`
}

// Init verifies platform ownership and seals a new root signing secret.
func (s *Service) Init(ctx context.Context, in InitInput) (InitResult, error) {
	email := strings.TrimSpace(in.Email)
	if email == "" {
		email = strings.TrimSpace(s.promptEmail())
	}
	if email == "" {
		return InitResult{}, clierr.Usage("owner email address required (use --owner <email>)")
	}
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return InitResult{}, clierr.Usage(fmt.Sprintf("invalid email address %q", email))
	}

	store := s.Vault
	if store.Exists() && !in.Force {
		if rec, err := store.Load(); err == nil && rec != nil && rec.IsVerified() {
			fmt.Fprintf(s.out(), "%s Platform ownership is already verified for %s (verified on %s).\n   Use --force to re-initialize and generate a new root signing secret.\n",
				s.icon(s.UI.Info),
				s.bold(rec.Email),
				s.subtle(rec.VerifiedAt.Format("2006-01-02 15:04:05 UTC")),
			)
			return InitResult{
				AlreadyVerified: true,
				Verified:        true,
				Email:           rec.Email,
				DisplayName:     rec.DisplayName,
				VerifiedAt:      rec.VerifiedAt.Format(time.RFC3339),
				KeyFingerprint:  rec.KeyFingerprint,
				VaultLocation:   store.FilePath(),
			}, nil
		}
	}

	if in.NoSend && strings.TrimSpace(in.Code) == "" {
		fmt.Fprintf(s.errOut(), "  %s  %s\n\n", s.UI.Error, s.errorText("Pre-flight Check Failed: --no-send requires an explicit verification code via --code."))
		fmt.Fprintf(s.errOut(), "  Terminal OTP display is disabled for security.\n")
		fmt.Fprintf(s.errOut(), "  When bypassing email delivery for testing, pass: orbit admin init --no-send --code <code>\n\n")
		return InitResult{}, clierr.Usage("--no-send requires an explicit verification code via --code")
	}

	code := strings.TrimSpace(in.Code)
	hermetic := in.NoSend && code != ""
	var (
		challenges *domain.ChallengeManager
		server     Server
	)

	if hermetic {
		challenges = domain.NewChallengeManager()
		if _, err := challenges.CreateChallengeWithCode(email, code, domain.DefaultChallengeTTL); err != nil {
			return InitResult{}, clierr.Wrap(err, clierr.Failed("failed to create challenge"))
		}
	} else {
		server = s.Server
		if server == nil {
			return InitResult{}, clierr.Failed("no Orbit server configured")
		}
		if len(domain.CleanCode(code)) != 8 {
			fmt.Fprintf(s.out(), "  %s  Connecting to Orbit server at %s...\n", s.icon(s.UI.Arrow), s.code(in.ServerURL))
			if _, err := server.InitiateChallenge(ctx, email); err != nil {
				return InitResult{}, withMessage(err, fmt.Sprintf("failed to initiate challenge on server %s", in.ServerURL))
			}
			fmt.Fprintf(s.out(), "  %s  Challenge accepted for %s (OTP is emailed only when the server is orbit-server with SMTP)\n",
				s.icon(s.UI.OK),
				s.bold(email),
			)
			if code == "" {
				code = strings.TrimSpace(s.promptCode())
			}
		}
	}

	if code == "" {
		return InitResult{}, clierr.Usage("verification code cannot be empty")
	}

	if hermetic {
		valid, err := challenges.VerifyCode(email, code)
		if err != nil {
			return InitResult{}, clierr.Wrap(err, clierr.Auth("OTP verification failed"))
		}
		if !valid {
			return InitResult{}, clierr.Auth("OTP verification failed: invalid code")
		}
	} else {
		verification, err := server.VerifyChallenge(ctx, email, code)
		if err != nil {
			return InitResult{}, withMessage(err, "remote OTP verification failed")
		}
		if verification.Status != "verified" {
			return InitResult{}, clierr.Failed(fmt.Sprintf("server rejected verification (status: %s)", verification.Status))
		}
	}

	secret, err := domain.GenerateMasterSecret()
	if err != nil {
		return InitResult{}, clierr.Wrap(err, clierr.Failed("failed to generate master signing secret"))
	}

	rec := &domain.OwnerRecord{
		Email:             email,
		DisplayName:       strings.TrimSpace(in.DisplayName),
		VerifiedAt:        s.now(),
		RootSigningSecret: secret,
		KeyFingerprint:    domain.ComputeFingerprint(secret),
	}
	if err := store.Save(rec); err != nil {
		return InitResult{}, clierr.Wrap(err, clierr.Failed("failed to save owner record"))
	}

	fmt.Fprintln(s.out(), s.title("Orbit Platform Ownership Verified"))
	fmt.Fprintf(s.out(), "  %s  Platform owner %s verified and root cryptographic vault sealed.\n\n",
		s.icon(s.UI.OK),
		s.bold(email),
	)
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Owner Email:"), s.bold(rec.Email))
	if rec.DisplayName != "" {
		fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Display Name:"), rec.DisplayName)
	}
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Verified At:"), rec.VerifiedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Key Fingerprint:"), s.code(rec.KeyFingerprint))
	fmt.Fprintf(s.out(), "  %-18s %s %s\n\n", s.header("Vault File:"), s.subtle(store.FilePath()), s.success("(0600 sealed)"))

	summary := "Master signing key generated and sealed in local vault (mode 0600).\nAll developer onboarding invitations and privileged operations\nwill be cryptographically signed by this verified owner identity."
	fmt.Fprintln(s.out(), s.card("OWNERSHIP SECURITY SUMMARY", summary))

	return InitResult{
		Verified:       true,
		Email:          rec.Email,
		DisplayName:    rec.DisplayName,
		VerifiedAt:     rec.VerifiedAt.Format(time.RFC3339),
		KeyFingerprint: rec.KeyFingerprint,
		VaultLocation:  store.FilePath(),
	}, nil
}

// VerifyInput is one `admin verify` invocation.
type VerifyInput struct {
	Email       string
	Code        string
	DisplayName string
	Force       bool
}

// VerifyResult is what `admin verify` produced.
type VerifyResult struct {
	AlreadyVerified bool   `json:"alreadyVerified"`
	Verified        bool   `json:"verified"`
	Email           string `json:"email"`
	DisplayName     string `json:"displayName,omitempty"`
	KeyFingerprint  string `json:"keyFingerprint,omitempty"`
	VaultLocation   string `json:"vaultLocation"`
}

// Verify seals the vault locally with an in-process OTP check and no API call.
func (s *Service) Verify(_ context.Context, in VerifyInput) (VerifyResult, error) {
	email := strings.TrimSpace(in.Email)
	if email == "" {
		email = strings.TrimSpace(s.promptEmail())
	}
	if email == "" {
		return VerifyResult{}, clierr.Usage("owner email address required")
	}
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return VerifyResult{}, clierr.Usage(fmt.Sprintf("invalid email address %q", email))
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		code = strings.TrimSpace(s.promptCode())
	}
	if code == "" {
		return VerifyResult{}, clierr.Usage("verification code cannot be empty")
	}

	store := s.Vault
	if store.Exists() && !in.Force {
		if rec, err := store.Load(); err == nil && rec != nil && rec.IsVerified() {
			fmt.Fprintf(s.out(), "%s Platform ownership already verified for %s. Use --force to re-verify.\n",
				s.icon(s.UI.Info),
				s.bold(rec.Email),
			)
			return VerifyResult{
				AlreadyVerified: true,
				Verified:        true,
				Email:           rec.Email,
				DisplayName:     rec.DisplayName,
				KeyFingerprint:  rec.KeyFingerprint,
				VaultLocation:   store.FilePath(),
			}, nil
		}
	}

	challenges := domain.NewChallengeManager()
	if _, err := challenges.CreateChallengeWithCode(email, code, domain.DefaultChallengeTTL); err != nil {
		return VerifyResult{}, clierr.Wrap(err, clierr.Usage("invalid challenge parameters"))
	}
	valid, err := challenges.VerifyCode(email, code)
	if err != nil {
		return VerifyResult{}, clierr.Wrap(err, clierr.Auth("verification failed"))
	}
	if !valid {
		return VerifyResult{}, clierr.Auth("verification failed: invalid code")
	}

	secret, err := domain.GenerateMasterSecret()
	if err != nil {
		return VerifyResult{}, clierr.Wrap(err, clierr.Failed("failed to generate master secret"))
	}

	rec := &domain.OwnerRecord{
		Email:             email,
		DisplayName:       strings.TrimSpace(in.DisplayName),
		VerifiedAt:        s.now(),
		RootSigningSecret: secret,
		KeyFingerprint:    domain.ComputeFingerprint(secret),
	}
	if err := store.Save(rec); err != nil {
		return VerifyResult{}, clierr.Wrap(err, clierr.Failed("failed to save owner record"))
	}

	fmt.Fprintln(s.out(), s.title("Orbit Platform Ownership Verified"))
	fmt.Fprintf(s.out(), "  %s  Owner %s verified and sealed in vault.\n", s.icon(s.UI.OK), s.bold(email))
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Key Fingerprint:"), s.code(rec.KeyFingerprint))
	fmt.Fprintf(s.out(), "  %-18s %s %s\n", s.header("Vault File:"), s.subtle(store.FilePath()), s.success("(0600 sealed)"))

	return VerifyResult{
		Verified:       true,
		Email:          rec.Email,
		DisplayName:    rec.DisplayName,
		KeyFingerprint: rec.KeyFingerprint,
		VaultLocation:  store.FilePath(),
	}, nil
}

// StatusResult is the documented `admin status --format json` object.
type StatusResult struct {
	Verified         bool   `json:"verified"`
	Email            string `json:"email,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	VerifiedAt       string `json:"verified_at,omitempty"`
	KeyFingerprint   string `json:"key_fingerprint,omitempty"`
	VaultLocation    string `json:"vault_location"`
	VaultPermissions string `json:"vault_permissions"`
	PermissionsValid bool   `json:"permissions_valid"`
	MailHost         string `json:"mail_host"`
}

// Status reports the local vault state without calling the server.
func (s *Service) Status() StatusResult {
	store := s.Vault
	rec, err := store.Load()
	permErr := store.CheckPermissions()

	mailHost := s.env("ORBIT_SMTP_HOST")
	if mailHost == "" {
		mailHost = s.env("SMTP_HOST")
	}
	if mailPort := s.env("ORBIT_SMTP_PORT"); mailPort != "" && !strings.Contains(mailHost, ":") {
		mailHost = fmt.Sprintf("%s:%s", mailHost, mailPort)
	} else if mailPort := s.env("SMTP_PORT"); mailPort != "" && !strings.Contains(mailHost, ":") {
		mailHost = fmt.Sprintf("%s:%s", mailHost, mailPort)
	}
	if mailHost == "" {
		mailHost = "mail.manova.space:587"
	}

	isVerified := err == nil && rec != nil && rec.IsVerified()

	permStr := "not found"
	permValid := false
	if store.Exists() {
		if permErr == nil {
			permStr = "0600 (secure)"
			permValid = true
		} else {
			permStr = "insecure (" + permErr.Error() + ")"
			permValid = false
		}
	}

	res := StatusResult{
		Verified:         isVerified,
		VaultLocation:    store.FilePath(),
		VaultPermissions: permStr,
		PermissionsValid: permValid,
		MailHost:         mailHost,
	}
	if isVerified {
		res.Email = rec.Email
		res.DisplayName = rec.DisplayName
		res.VerifiedAt = rec.VerifiedAt.Format(time.RFC3339)
		res.KeyFingerprint = rec.KeyFingerprint
	}
	return res
}

// WriteStatus renders the human form of the status report.
func (s *Service) WriteStatus(res StatusResult) {
	fmt.Fprintln(s.out(), s.title("Orbit Server Ownership Status"))

	if !res.Verified {
		fmt.Fprintf(s.out(), "  %s  %s\n\n", s.icon(s.UI.Warn), s.warning("Platform ownership is UNVERIFIED."))
		fmt.Fprintf(s.out(), "  Run '%s' to verify server ownership.\n\n", s.bold("orbit admin init --owner <email>"))
		fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Vault Location:"), s.subtle(res.VaultLocation))
		fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Vault Perms:"), s.subtle(res.VaultPermissions))
		fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Mail Gateway:"), s.code(res.MailHost))
		return
	}

	fmt.Fprintf(s.out(), "  %s  %s\n\n", s.icon(s.UI.OK), s.success("Platform ownership is VERIFIED."))
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Owner Email:"), s.bold(res.Email))
	if res.DisplayName != "" {
		fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Display Name:"), res.DisplayName)
	}
	verifiedAt, _ := time.Parse(time.RFC3339, res.VerifiedAt)
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Verified At:"), verifiedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Key Fingerprint:"), s.code(res.KeyFingerprint))
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Vault Location:"), s.subtle(res.VaultLocation))

	permDisplay := s.success("0600 (secure)")
	if !res.PermissionsValid {
		permDisplay = s.errorText(res.VaultPermissions)
	}
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Permissions:"), permDisplay)
	fmt.Fprintf(s.out(), "  %-18s %s\n", s.header("Mail Gateway:"), s.code(res.MailHost))
}

// RotateInput is one `admin rotate-secret` invocation.
type RotateInput struct {
	Yes bool
}

// RotateResult is what `admin rotate-secret` produced.
type RotateResult struct {
	Cancelled      bool   `json:"cancelled"`
	Email          string `json:"email,omitempty"`
	OldFingerprint string `json:"previousFingerprint,omitempty"`
	KeyFingerprint string `json:"keyFingerprint,omitempty"`
	VaultLocation  string `json:"vaultLocation,omitempty"`
}

// RotateSecret replaces the root signing secret in the sealed vault.
func (s *Service) RotateSecret(_ context.Context, in RotateInput) (RotateResult, error) {
	store := s.Vault
	rec, err := store.Load()
	if err != nil || rec == nil || !rec.IsVerified() {
		return RotateResult{}, clierr.Auth("cannot rotate signing secret: platform owner is not verified (run 'orbit admin init' first)")
	}

	if !in.Yes {
		confirm := false
		if s.Prompts.ConfirmRotate != nil {
			confirm = s.Prompts.ConfirmRotate(rec.Email)
		}
		if !confirm {
			fmt.Fprintln(s.out(), "Secret rotation cancelled.")
			return RotateResult{Cancelled: true, Email: rec.Email, VaultLocation: store.FilePath()}, nil
		}
	}

	newSecret, err := domain.GenerateMasterSecret()
	if err != nil {
		return RotateResult{}, clierr.Wrap(err, clierr.Failed("failed to generate new master signing secret"))
	}

	oldFingerprint := rec.KeyFingerprint
	rec.RootSigningSecret = newSecret
	rec.KeyFingerprint = domain.ComputeFingerprint(newSecret)
	rec.VerifiedAt = s.now()

	if err := store.Save(rec); err != nil {
		return RotateResult{}, clierr.Wrap(err, clierr.Failed("failed to save rotated owner record"))
	}

	fmt.Fprintln(s.out(), s.title("Master Signing Secret Rotated"))
	fmt.Fprintf(s.out(), "  %s  %s\n\n", s.icon(s.UI.OK), s.success("Cryptographic root secret successfully rotated and sealed."))
	fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Owner Email:"), s.bold(rec.Email))
	if oldFingerprint != "" {
		fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Previous Fingerprint:"), s.subtle(oldFingerprint))
	}
	fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("New Fingerprint:"), s.code(rec.KeyFingerprint))
	fmt.Fprintf(s.out(), "  %-22s %s\n\n", s.header("Vault File:"), s.subtle(store.FilePath()))
	fmt.Fprintf(s.out(), "  %s  %s\n", s.icon(s.UI.Warn), s.warning("Notice: All onboarding invite tokens signed with the previous secret are now invalidated."))

	return RotateResult{
		Email:          rec.Email,
		OldFingerprint: oldFingerprint,
		KeyFingerprint: rec.KeyFingerprint,
		VaultLocation:  store.FilePath(),
	}, nil
}

// GrantInput is one `admin grant` invocation.
type GrantInput struct {
	Email     string
	Role      string
	Code      string
	TTL       time.Duration
	ServerURL string
	Send      bool
	Telegram  bool
	Quiet     bool
}

// GrantResult is what `admin grant` produced.
type GrantResult struct {
	Status       string `json:"status"`
	Email        string `json:"email"`
	Code         string `json:"code"`
	Role         string `json:"role"`
	ExpiresAt    string `json:"expires_at"`
	TelegramSent bool   `json:"telegram_sent"`
	EmailSent    bool   `json:"email_sent"`
}

// Grant generates and optionally delivers a single-use admin authorization code.
func (s *Service) Grant(ctx context.Context, in GrantInput) (GrantResult, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || !strings.Contains(email, "@") {
		return GrantResult{}, clierr.Usage(fmt.Sprintf("invalid recipient email address %q", in.Email))
	}

	rec, err := s.Vault.Load()
	if err != nil || rec == nil || !rec.IsVerified() {
		return GrantResult{}, clierr.Auth("cannot generate grant: platform owner is not verified on this machine (run 'orbit admin init' first)")
	}

	ttl := in.TTL
	if ttl <= 0 {
		ttl = domain.DefaultGrantTTL
	}
	role := in.Role
	if role == "" {
		role = domain.DefaultGrantRole
	}

	var codeFormatted string
	if in.Code != "" {
		codeFormatted = domain.Format8DigitCode(in.Code)
		clean := domain.CleanCode(in.Code)
		if len(clean) != 8 {
			return GrantResult{}, clierr.Usage(fmt.Sprintf("grant code must be exactly 8 digits, got %d", len(clean)))
		}
	} else {
		codeFormatted, err = domain.Generate8DigitCode()
		if err != nil {
			return GrantResult{}, clierr.Wrap(err, clierr.Failed("failed to generate grant code"))
		}
	}

	expiresAt := s.now().Add(ttl)
	if s.Server != nil {
		registration, regErr := s.Server.CreateGrant(ctx, email, role, codeFormatted, rec.RootSigningSecret, ttl)
		if regErr != nil && !in.Quiet {
			fmt.Fprintf(s.errOut(), "  %s  Notice: server grant registration: %v\n", s.icon(s.UI.Info), regErr)
		} else if regErr == nil && !registration.ExpiresAt.IsZero() {
			expiresAt = registration.ExpiresAt
		}
	}

	telegramSent := false
	if in.Telegram && s.Telegram != nil {
		if err := s.Telegram.SendGrant(ctx, email, codeFormatted, role, expiresAt); err != nil {
			fmt.Fprintf(s.errOut(), "  %s  Telegram dispatch failed: %v\n", s.icon(s.UI.Error), err)
		} else {
			telegramSent = true
		}
	}

	emailSent := false
	if in.Send {
		if s.MailerFactory == nil {
			fmt.Fprintf(s.errOut(), "  %s  Email dispatch failed: %v\n", s.icon(s.UI.Error), errors.New("no mailer configured"))
		} else {
			mailer := s.MailerFactory(s.smtpConfig())
			data := ChallengeEmail{
				OwnerEmail:  email,
				OTPCode:     codeFormatted,
				ExpiresIn:   fmt.Sprintf("%d minutes", int(ttl.Minutes())),
				ServerHost:  in.ServerURL,
				GeneratedAt: s.now(),
			}
			if err := mailer.SendOwnerChallenge(ctx, email, data); err != nil {
				fmt.Fprintf(s.errOut(), "  %s  Email dispatch failed: %v\n", s.icon(s.UI.Error), err)
			} else {
				emailSent = true
			}
		}
	}

	if in.Quiet {
		return GrantResult{
			Status:       "grant_generated",
			Email:        email,
			Code:         codeFormatted,
			Role:         role,
			ExpiresAt:    expiresAt.Format(time.RFC3339),
			TelegramSent: telegramSent,
			EmailSent:    emailSent,
		}, nil
	}

	fmt.Fprintln(s.out(), s.title("Orbit Administrator Grant Generated"))
	fmt.Fprintf(s.out(), "  %s  %s\n\n", s.icon(s.UI.OK), s.success(fmt.Sprintf("Single-use 8-digit grant generated for %s", email)))
	fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Grant Code:"), s.bold(codeFormatted))
	fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Recipient:"), s.subtle(email))
	fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Assigned Role:"), s.code(role))
	fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Expires In:"), s.subtle(fmt.Sprintf("%d minutes (%s)", int(ttl.Minutes()), expiresAt.Format("15:04:05 UTC"))))
	if telegramSent {
		fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Telegram:"), s.success("Dispatched to Secrets Topic"))
	}
	if emailSent {
		fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Email Delivery:"), s.success("Dispatched via SMTP"))
	}
	fmt.Fprintf(s.out(), "\n  %s %s\n", s.bold("Instructions for recipient:"), "")
	fmt.Fprintf(s.out(), "    orbit admin init %s --code %s\n\n", email, codeFormatted)

	return GrantResult{
		Status:       "grant_generated",
		Email:        email,
		Code:         codeFormatted,
		Role:         role,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		TelegramSent: telegramSent,
		EmailSent:    emailSent,
	}, nil
}

func (s *Service) smtpConfig() SMTPConfig {
	host := s.env("ORBIT_SMTP_HOST")
	if host == "" {
		host = s.env("SMTP_HOST")
	}
	if host == "" {
		host = "mail.manova.space"
	}
	port := s.env("ORBIT_SMTP_PORT")
	if port == "" {
		port = s.env("SMTP_PORT")
	}
	if port == "" {
		port = "587"
	}
	user := s.env("ORBIT_SMTP_USER")
	if user == "" {
		user = s.env("SMTP_USER")
	}
	pass := s.env("ORBIT_SMTP_PASS")
	if pass == "" {
		pass = s.env("SMTP_PASS")
	}
	from := s.env("ORBIT_SMTP_FROM")
	if from == "" {
		from = s.env("SMTP_FROM")
	}
	if from == "" {
		from = "Orbit Platform <noreply@manova.space>"
	}
	return SMTPConfig{Host: host, Port: port, User: user, Pass: pass, From: from}
}

// ResetTOTPInput is one `admin totp reset` invocation.
type ResetTOTPInput struct {
	Email string
}

// ResetTOTPResult is what `admin totp reset` produced.
type ResetTOTPResult struct {
	Status       string `json:"status"`
	Email        string `json:"email"`
	RecoveryCode string `json:"recovery_code"`
	ExpiresIn    string `json:"expires_in"`
}

// ResetTOTP issues a fresh two-factor recovery code for a user.
func (s *Service) ResetTOTP(_ context.Context, in ResetTOTPInput) (ResetTOTPResult, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || !strings.Contains(email, "@") {
		return ResetTOTPResult{}, clierr.Usage(fmt.Sprintf("invalid email address %q", in.Email))
	}

	rec, err := s.Vault.Load()
	if err != nil || rec == nil || !rec.IsVerified() {
		return ResetTOTPResult{}, clierr.Auth("cannot reset TOTP: platform owner is not verified (run 'orbit admin init' first)")
	}

	code, err := domain.Generate8DigitCode()
	if err != nil {
		return ResetTOTPResult{}, clierr.Wrap(err, clierr.Failed("failed to generate recovery code"))
	}

	result := ResetTOTPResult{
		Status:       "totp_reset",
		Email:        email,
		RecoveryCode: code,
		ExpiresIn:    "15m",
	}

	if s.Out != nil {
		fmt.Fprintln(s.out(), s.title("User TOTP Reset & Recovery Issued"))
		fmt.Fprintf(s.out(), "  %s  %s\n\n", s.icon(s.UI.OK), s.success(fmt.Sprintf("TOTP successfully reset for %s", email)))
		fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Recovery Code:"), s.bold(code))
		fmt.Fprintf(s.out(), "  %-22s %s\n", s.header("Expires In:"), s.subtle("15 minutes"))
		fmt.Fprintf(s.out(), "\n  %s %s\n", s.bold("Instructions:"), "")
		fmt.Fprintf(s.out(), "    orbit admin init %s --code %s\n\n", email, code)
	}

	return result, nil
}

// withMessage prefixes msg while keeping the error's clierr code when it has one.
func withMessage(err error, msg string) error {
	var c *clierr.Error
	if errors.As(err, &c) {
		return &clierr.Error{Code: c.Code, Exit: c.Exit, Message: msg, Hint: c.Hint, Err: err}
	}
	return clierr.Wrap(err, clierr.Failed(msg))
}
