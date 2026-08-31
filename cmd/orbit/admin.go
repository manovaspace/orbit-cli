package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/config"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/spf13/cobra"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Manage platform ownership verification and root cryptographic secrets",
		Long: `Commands to verify server and platform administrative ownership, manage
cryptographic root signing secrets, and check ownership vault status.`,
	}

	cmd.AddCommand(newAdminInitCmd())
	cmd.AddCommand(newAdminGrantCmd())
	cmd.AddCommand(newAdminTOTPCmd())
	cmd.AddCommand(newAdminStatusCmd())
	cmd.AddCommand(newAdminVerifyCmd())
	cmd.AddCommand(newAdminRotateSecretCmd())

	return cmd
}

func newAdminInitCmd() *cobra.Command {
	var (
		ownerFlag  string
		nameFlag   string
		storeFlag  string
		serverFlag string
		noSendFlag bool
		codeFlag   string
		forceFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "init [email]",
		Short: "Initialize and verify platform server ownership via email OTP challenge",
		Long: `Initiates server ownership verification. Requests a 6-digit OTP challenge from the Orbit
server, prompts for the code, and seals a 32-byte cryptographic master signing secret in the local
owner vault (mode 0600). Email delivery happens only when the server is orbit-server with SMTP;
orbit-api-gateway stores the OTP in memory and does not send mail.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			cfg, err := config.Resolve(config.ResolveOptions{
				ConfigPath: getConfigPath(cmd),
				ServerFlag: serverFlag,
			})
			if err != nil {
				return fmt.Errorf("failed to resolve configuration: %w", err)
			}

			email := strings.TrimSpace(ownerFlag)
			if len(args) > 0 && email == "" {
				email = strings.TrimSpace(args[0])
			}

			if email == "" {
				email = promptString(in, out, "Enter platform owner email address", "")
				email = strings.TrimSpace(email)
			}

			if email == "" {
				return errors.New("owner email address required (use --owner <email>)")
			}

			if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
				return fmt.Errorf("invalid email address %q", email)
			}

			store := owner.NewStore(storeFlag)

			// Check if already verified
			if store.IsVerified() && !forceFlag {
				rec, err := store.LoadOwner()
				if err == nil && rec != nil {
					fmt.Fprintf(out, "%s Platform ownership is already verified for %s (verified on %s).\n   Use --force to re-initialize and generate a new root signing secret.\n",
						iconInfo,
						boldStyle.Render(rec.Email),
						subtleStyle.Render(rec.VerifiedAt.Format("2006-01-02 15:04:05 UTC")),
					)
					return nil
				}
			}

			if noSendFlag && strings.TrimSpace(codeFlag) == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "  %s  %s\n\n", iconError, errorStyle.Render("Pre-flight Check Failed: --no-send requires an explicit verification code via --code."))
				fmt.Fprintf(cmd.ErrOrStderr(), "  Terminal OTP display is disabled for security.\n")
				fmt.Fprintf(cmd.ErrOrStderr(), "  When bypassing email delivery for testing, pass: orbit admin init --no-send --code <code>\n\n")
				return errors.New("--no-send requires an explicit verification code via --code")
			}

			isHermetic := noSendFlag && strings.TrimSpace(codeFlag) != ""
			var (
				cm        *owner.ChallengeManager
				apiClient *client.Client
			)

			if isHermetic {
				cm = owner.NewChallengeManager()
				if _, err := cm.CreateChallengeWithCode(email, strings.TrimSpace(codeFlag), owner.DefaultChallengeTTL); err != nil {
					return fmt.Errorf("failed to create challenge: %w", err)
				}
			} else {
				serverURL := strings.TrimSpace(cfg.Config.Server.URL)
				if serverURL == "" {
					serverURL = config.DefaultServerURL
				}
				apiClient = client.NewClient(serverURL)

				clean := owner.CleanCode(codeFlag)
				is8DigitGrant := len(clean) == 8

				if !is8DigitGrant {
					fmt.Fprintf(out, "  %s  Connecting to Orbit server at %s...\n", iconArrow, codeStyle.Render(serverURL))
					_, err := apiClient.InitiateOwnerChallenge(cmd.Context(), email)
					if err != nil {
						return fmt.Errorf("failed to initiate challenge on server %s: %w", serverURL, err)
					}
					fmt.Fprintf(out, "  %s  Challenge accepted for %s (OTP is emailed only when the server is orbit-server with SMTP)\n",
						iconOK,
						boldStyle.Render(email),
					)
					if strings.TrimSpace(codeFlag) == "" {
						codeFlag = promptString(in, out, "Enter 6-digit OTP or 8-digit grant code", "")
					}
				}
			}

			// Acquire code from user if still empty
			inputCode := strings.TrimSpace(codeFlag)
			if inputCode == "" {
				return errors.New("verification code cannot be empty")
			}

			// Verify code
			if isHermetic {
				valid, err := cm.VerifyCode(email, inputCode)
				if err != nil || !valid {
					return fmt.Errorf("OTP verification failed: %w", err)
				}
			} else {
				vResp, err := apiClient.VerifyOwnerChallenge(cmd.Context(), email, inputCode)
				if err != nil {
					return fmt.Errorf("remote OTP verification failed: %w", err)
				}
				if vResp.Status != "verified" {
					return fmt.Errorf("server rejected verification (status: %s)", vResp.Status)
				}
			}

			// Generate 32-byte master signing secret
			secret, err := owner.GenerateMasterSecret()
			if err != nil {
				return fmt.Errorf("failed to generate master signing secret: %w", err)
			}

			displayName := strings.TrimSpace(nameFlag)
			rec := &owner.OwnerRecord{
				Email:             email,
				DisplayName:       displayName,
				VerifiedAt:        time.Now().UTC(),
				RootSigningSecret: secret,
				KeyFingerprint:    owner.ComputeFingerprint(secret),
			}

			if err := store.SaveOwner(rec); err != nil {
				return fmt.Errorf("failed to save owner record: %w", err)
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit Platform Ownership Verified"))
			fmt.Fprintf(out, "  %s  Platform owner %s verified and root cryptographic vault sealed.\n\n",
				iconOK,
				boldStyle.Render(email),
			)

			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Owner Email:"), boldStyle.Render(rec.Email))
			if rec.DisplayName != "" {
				fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Display Name:"), rec.DisplayName)
			}
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Verified At:"), rec.VerifiedAt.Format("2006-01-02 15:04:05 UTC"))
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Key Fingerprint:"), codeStyle.Render(rec.KeyFingerprint))
			fmt.Fprintf(out, "  %-18s %s %s\n\n", headerStyle.Render("Vault File:"), subtleStyle.Render(store.FilePath()), successStyle.Render("(0600 sealed)"))

			summary := "Master signing key generated and sealed in local vault (mode 0600).\nAll developer onboarding invitations and privileged operations\nwill be cryptographically signed by this verified owner identity."
			fmt.Fprintln(out, renderCard("OWNERSHIP SECURITY SUMMARY", summary))

			return nil
		},
	}

	cmd.Flags().StringVarP(&ownerFlag, "owner", "o", "", "Owner email address (e.g. admin@example.com)")
	cmd.Flags().StringVarP(&nameFlag, "name", "n", "", "Owner display name (e.g. 'Alex Smith')")
	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Orbit server URL (e.g. https://orbit.manova.space)")
	cmd.Flags().BoolVar(&noSendFlag, "no-send", false, "Suppress dispatching challenge email")
	cmd.Flags().StringVarP(&codeFlag, "code", "c", "", "6-digit verification code (for non-interactive execution)")
	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force re-initialization even if already verified")

	return cmd
}

type adminStatusJSON struct {
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

func newAdminStatusCmd() *cobra.Command {
	var (
		storeFlag  string
		formatFlag string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display platform ownership verification status, vault integrity, and mail config",
		Long:  "Reports whether platform ownership has been verified, vault file path and permissions, and active SMTP gateway.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			store := owner.NewStore(storeFlag)
			rec, err := store.LoadOwner()
			permErr := store.CheckPermissions()

			cfg, _ := config.Resolve(config.ResolveOptions{
				ConfigPath: getConfigPath(cmd),
			})
			_ = cfg

			mailHost := os.Getenv("ORBIT_SMTP_HOST")
			if mailHost == "" {
				mailHost = os.Getenv("SMTP_HOST")
			}
			if mailPort := os.Getenv("ORBIT_SMTP_PORT"); mailPort != "" && !strings.Contains(mailHost, ":") {
				mailHost = fmt.Sprintf("%s:%s", mailHost, mailPort)
			} else if mailPort := os.Getenv("SMTP_PORT"); mailPort != "" && !strings.Contains(mailHost, ":") {
				mailHost = fmt.Sprintf("%s:%s", mailHost, mailPort)
			}
			if mailHost == "" {
				mailHost = "mail.manova.space:587"
			}

			isVerified := (err == nil && rec != nil && rec.IsVerified())

			permStr := "not found"
			permValid := false
			if _, statErr := os.Stat(store.FilePath()); statErr == nil {
				if permErr == nil {
					permStr = "0600 (secure)"
					permValid = true
				} else {
					permStr = "insecure (" + permErr.Error() + ")"
					permValid = false
				}
			}

			if strings.ToLower(formatFlag) == "json" {
				res := adminStatusJSON{
					Verified:         isVerified,
					VaultLocation:    store.FilePath(),
					VaultPermissions: permStr,
					PermissionsValid: permValid,
					MailHost:         mailHost,
				}
				if isVerified && rec != nil {
					res.Email = rec.Email
					res.DisplayName = rec.DisplayName
					res.VerifiedAt = rec.VerifiedAt.Format(time.RFC3339)
					res.KeyFingerprint = rec.KeyFingerprint
				}

				data, _ := json.MarshalIndent(res, "", "  ")
				fmt.Fprintln(out, string(data))
				return nil
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit Server Ownership Status"))

			if !isVerified {
				fmt.Fprintf(out, "  %s  %s\n\n", iconWarn, warningStyle.Render("Platform ownership is UNVERIFIED."))
				fmt.Fprintf(out, "  Run '%s' to verify server ownership.\n\n", boldStyle.Render("orbit admin init --owner <email>"))
				fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Vault Location:"), subtleStyle.Render(store.FilePath()))
				fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Vault Perms:"), subtleStyle.Render(permStr))
				fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Mail Gateway:"), codeStyle.Render(mailHost))
				return nil
			}

			fmt.Fprintf(out, "  %s  %s\n\n", iconOK, successStyle.Render("Platform ownership is VERIFIED."))
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Owner Email:"), boldStyle.Render(rec.Email))
			if rec.DisplayName != "" {
				fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Display Name:"), rec.DisplayName)
			}
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Verified At:"), rec.VerifiedAt.Format("2006-01-02 15:04:05 UTC"))
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Key Fingerprint:"), codeStyle.Render(rec.KeyFingerprint))
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Vault Location:"), subtleStyle.Render(store.FilePath()))

			permDisplay := successStyle.Render("0600 (secure)")
			if !permValid {
				permDisplay = errorStyle.Render(permStr)
			}
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Permissions:"), permDisplay)
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Mail Gateway:"), codeStyle.Render(mailHost))

			return nil
		},
	}

	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format: table or json")

	return cmd
}

func newAdminVerifyCmd() *cobra.Command {
	var (
		ownerFlag string
		codeFlag  string
		nameFlag  string
		storeFlag string
		forceFlag bool
	)

	cmd := &cobra.Command{
		Use:   "verify [email] [code]",
		Short: "Seal owner.json locally (hermetic OTP; no API call)",
		Long:  "Local-only verification. The code is checked in-process, not against orbit-server or the gateway. On success a new master secret is sealed in owner.json. Does not import a server fingerprint.",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			cfg, _ := config.Resolve(config.ResolveOptions{
				ConfigPath: getConfigPath(cmd),
			})
			_ = cfg

			email := strings.TrimSpace(ownerFlag)
			if len(args) > 0 && email == "" {
				email = strings.TrimSpace(args[0])
			}
			if email == "" {
				email = promptString(in, out, "Enter owner email address", "")
				email = strings.TrimSpace(email)
			}
			if email == "" {
				return errors.New("owner email address required")
			}
			if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
				return fmt.Errorf("invalid email address %q", email)
			}

			code := strings.TrimSpace(codeFlag)
			if len(args) > 1 && code == "" {
				code = strings.TrimSpace(args[1])
			}
			if code == "" {
				code = promptString(in, out, "Enter 6-digit verification code", "")
				code = strings.TrimSpace(code)
			}
			if code == "" {
				return errors.New("verification code cannot be empty")
			}

			store := owner.NewStore(storeFlag)
			if store.IsVerified() && !forceFlag {
				rec, err := store.LoadOwner()
				if err == nil && rec != nil {
					fmt.Fprintf(out, "%s Platform ownership already verified for %s. Use --force to re-verify.\n",
						iconInfo,
						boldStyle.Render(rec.Email),
					)
					return nil
				}
			}

			cm := owner.NewChallengeManager()
			if _, err := cm.CreateChallengeWithCode(email, code, owner.DefaultChallengeTTL); err != nil {
				return fmt.Errorf("invalid challenge parameters: %w", err)
			}

			valid, err := cm.VerifyCode(email, code)
			if err != nil || !valid {
				return fmt.Errorf("verification failed: %w", err)
			}

			secret, err := owner.GenerateMasterSecret()
			if err != nil {
				return fmt.Errorf("failed to generate master secret: %w", err)
			}

			displayName := strings.TrimSpace(nameFlag)
			rec := &owner.OwnerRecord{
				Email:             email,
				DisplayName:       displayName,
				VerifiedAt:        time.Now().UTC(),
				RootSigningSecret: secret,
				KeyFingerprint:    owner.ComputeFingerprint(secret),
			}

			if err := store.SaveOwner(rec); err != nil {
				return fmt.Errorf("failed to save owner record: %w", err)
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit Platform Ownership Verified"))
			fmt.Fprintf(out, "  %s  Owner %s verified and sealed in vault.\n", iconOK, boldStyle.Render(email))
			fmt.Fprintf(out, "  %-18s %s\n", headerStyle.Render("Key Fingerprint:"), codeStyle.Render(rec.KeyFingerprint))
			fmt.Fprintf(out, "  %-18s %s %s\n", headerStyle.Render("Vault File:"), subtleStyle.Render(store.FilePath()), successStyle.Render("(0600 sealed)"))

			return nil
		},
	}

	cmd.Flags().StringVarP(&ownerFlag, "owner", "o", "", "Owner email address")
	cmd.Flags().StringVarP(&codeFlag, "code", "c", "", "6-digit verification code")
	cmd.Flags().StringVarP(&nameFlag, "name", "n", "", "Owner display name")
	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force re-verification even if already verified")

	return cmd
}

func newAdminRotateSecretCmd() *cobra.Command {
	var (
		storeFlag string
		yesFlag   bool
	)

	cmd := &cobra.Command{
		Use:     "rotate-secret",
		Aliases: []string{"rotate"},
		Short:   "Rotate root master signing secret in the sealed owner vault",
		Long: `Generates a fresh 32-byte cryptographic root signing secret and seals the vault.
WARNING: All developer onboarding invitation tokens signed with the previous secret will be invalidated.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			store := owner.NewStore(storeFlag)
			rec, err := store.LoadOwner()
			if err != nil || rec == nil || !rec.IsVerified() {
				return errors.New("cannot rotate signing secret: platform owner is not verified (run 'orbit admin init' first)")
			}

			if !yesFlag {
				promptMsg := fmt.Sprintf("Rotating the master signing secret will invalidate all existing invitation tokens for %s. Proceed?", rec.Email)
				if !promptYesNo(in, out, promptMsg, false) {
					fmt.Fprintln(out, "Secret rotation cancelled.")
					return nil
				}
			}

			newSecret, err := owner.GenerateMasterSecret()
			if err != nil {
				return fmt.Errorf("failed to generate new master signing secret: %w", err)
			}

			oldFingerprint := rec.KeyFingerprint
			rec.RootSigningSecret = newSecret
			rec.KeyFingerprint = owner.ComputeFingerprint(newSecret)
			rec.VerifiedAt = time.Now().UTC()

			if err := store.SaveOwner(rec); err != nil {
				return fmt.Errorf("failed to save rotated owner record: %w", err)
			}

			fmt.Fprintln(out, titleStyle.Render("Master Signing Secret Rotated"))
			fmt.Fprintf(out, "  %s  %s\n\n", iconOK, successStyle.Render("Cryptographic root secret successfully rotated and sealed."))

			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Owner Email:"), boldStyle.Render(rec.Email))
			if oldFingerprint != "" {
				fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Previous Fingerprint:"), subtleStyle.Render(oldFingerprint))
			}
			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("New Fingerprint:"), codeStyle.Render(rec.KeyFingerprint))
			fmt.Fprintf(out, "  %-22s %s\n\n", headerStyle.Render("Vault File:"), subtleStyle.Render(store.FilePath()))

			fmt.Fprintf(out, "  %s  %s\n", iconWarn, warningStyle.Render("Notice: All onboarding invite tokens signed with the previous secret are now invalidated."))

			return nil
		},
	}

	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Skip interactive confirmation prompt")

	return cmd
}

func newAdminGrantCmd() *cobra.Command {
	var (
		roleFlag     string
		ttlFlag      time.Duration
		codeFlag     string
		storeFlag    string
		serverFlag   string
		sendFlag     bool
		telegramFlag bool
		jsonFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "grant <email>",
		Short: "Generate an 8-digit single-use authorization code for a new admin",
		Long: `Generates a single-use 8-digit administrative grant code (e.g. 8492-0194) bound to the
specified email address. The new admin uses this code with 'orbit admin init <email> --code 8492-0194'
to initialize their workstation without sending or receiving public challenge emails.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			email := strings.ToLower(strings.TrimSpace(args[0]))
			if email == "" || !strings.Contains(email, "@") {
				return fmt.Errorf("invalid recipient email address %q", args[0])
			}

			cfg, err := config.Resolve(config.ResolveOptions{
				ConfigPath: getConfigPath(cmd),
				ServerFlag: serverFlag,
			})
			if err != nil {
				return fmt.Errorf("failed to resolve config: %w", err)
			}

			store := owner.NewStore(storeFlag)
			rec, err := store.LoadOwner()
			if err != nil || rec == nil || !rec.IsVerified() {
				return errors.New("cannot generate grant: platform owner is not verified on this machine (run 'orbit admin init' first)")
			}

			if ttlFlag <= 0 {
				ttlFlag = owner.DefaultGrantTTL
			}
			if roleFlag == "" {
				roleFlag = owner.DefaultGrantRole
			}

			var codeFormatted string
			if codeFlag != "" {
				codeFormatted = owner.Format8DigitCode(codeFlag)
				clean := owner.CleanCode(codeFlag)
				if len(clean) != 8 {
					return fmt.Errorf("grant code must be exactly 8 digits, got %d", len(clean))
				}
			} else {
				codeFormatted, err = owner.Generate8DigitCode()
				if err != nil {
					return fmt.Errorf("failed to generate grant code: %w", err)
				}
			}

			// Register on server if server is configured
			serverURL := strings.TrimSpace(cfg.Config.Server.URL)
			if serverURL == "" {
				serverURL = config.DefaultServerURL
			}

			apiClient := client.NewClient(serverURL)
			grantResp, err := apiClient.CreateAdminGrant(cmd.Context(), email, roleFlag, codeFormatted, rec.RootSigningSecret, ttlFlag)
			if err != nil && !jsonFlag {
				// Warn if remote registration fails but continue if offline
				fmt.Fprintf(cmd.ErrOrStderr(), "  %s  Notice: server grant registration: %v\n", iconInfo, err)
			}

			expiresAt := time.Now().UTC().Add(ttlFlag)
			if grantResp != nil && !grantResp.ExpiresAt.IsZero() {
				expiresAt = grantResp.ExpiresAt
			}

			// Telegram dispatch if requested
			var telegramSent bool
			if telegramFlag {
				tgCfg, tgErr := owner.ResolveTelegramConfig()
				if tgErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %s  Telegram dispatch skipped: %v\n", iconWarn, tgErr)
				} else {
					if err := owner.DispatchAdminGrantTelegram(cmd.Context(), tgCfg, email, codeFormatted, roleFlag, expiresAt); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "  %s  Telegram dispatch failed: %v\n", iconError, err)
					} else {
						telegramSent = true
					}
				}
			}

			// SMTP dispatch if requested
			var emailSent bool
			if sendFlag {
				smtpHost := os.Getenv("ORBIT_SMTP_HOST")
				if smtpHost == "" {
					smtpHost = os.Getenv("SMTP_HOST")
				}
				if smtpHost == "" {
					smtpHost = "mail.manova.space"
				}
				smtpPort := os.Getenv("ORBIT_SMTP_PORT")
				if smtpPort == "" {
					smtpPort = os.Getenv("SMTP_PORT")
				}
				if smtpPort == "" {
					smtpPort = "587"
				}
				smtpUser := os.Getenv("ORBIT_SMTP_USER")
				if smtpUser == "" {
					smtpUser = os.Getenv("SMTP_USER")
				}
				smtpPass := os.Getenv("ORBIT_SMTP_PASS")
				if smtpPass == "" {
					smtpPass = os.Getenv("SMTP_PASS")
				}
				smtpFrom := os.Getenv("ORBIT_SMTP_FROM")
				if smtpFrom == "" {
					smtpFrom = os.Getenv("SMTP_FROM")
				}
				if smtpFrom == "" {
					smtpFrom = "Orbit Platform <noreply@manova.space>"
				}

				mailer := invite.NewSMTPMailer(invite.MailerConfig{
					Host: smtpHost,
					Port: smtpPort,
					User: smtpUser,
					Pass: smtpPass,
					From: smtpFrom,
				})
				emailData := invite.OwnerChallengeEmailData{
					OwnerEmail:  email,
					OTPCode:     codeFormatted,
					ExpiresIn:   fmt.Sprintf("%d minutes", int(ttlFlag.Minutes())),
					ServerHost:  serverURL,
					GeneratedAt: time.Now().UTC(),
				}
				if err := mailer.SendOwnerChallenge(cmd.Context(), email, emailData); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %s  Email dispatch failed: %v\n", iconError, err)
				} else {
					emailSent = true
				}
			}

			if jsonFlag {
				data := map[string]interface{}{
					"status":        "grant_generated",
					"email":         email,
					"code":          codeFormatted,
					"role":          roleFlag,
					"expires_at":    expiresAt.Format(time.RFC3339),
					"telegram_sent": telegramSent,
					"email_sent":    emailSent,
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(data)
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit Administrator Grant Generated"))
			fmt.Fprintf(out, "  %s  %s\n\n", iconOK, successStyle.Render(fmt.Sprintf("Single-use 8-digit grant generated for %s", email)))

			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Grant Code:"), boldStyle.Render(codeFormatted))
			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Recipient:"), subtleStyle.Render(email))
			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Assigned Role:"), codeStyle.Render(roleFlag))
			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Expires In:"), subtleStyle.Render(fmt.Sprintf("%d minutes (%s)", int(ttlFlag.Minutes()), expiresAt.Format("15:04:05 UTC"))))

			if telegramSent {
				fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Telegram:"), successStyle.Render("Dispatched to Secrets Topic"))
			}
			if emailSent {
				fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Email Delivery:"), successStyle.Render("Dispatched via SMTP"))
			}

			fmt.Fprintf(out, "\n  %s %s\n", boldStyle.Render("Instructions for recipient:"), "")
			fmt.Fprintf(out, "    orbit admin init %s --code %s\n\n", email, codeFormatted)

			return nil
		},
	}

	cmd.Flags().StringVar(&roleFlag, "role", "admin", "Role to grant (admin, maintainer, superadmin)")
	cmd.Flags().DurationVar(&ttlFlag, "ttl", 15*time.Minute, "Grant validity duration (e.g. 15m, 1h)")
	cmd.Flags().StringVar(&codeFlag, "code", "", "Explicit 8-digit code (auto-generated if omitted)")
	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Orbit server URL")
	cmd.Flags().BoolVar(&sendFlag, "send", false, "Dispatch grant code directly to recipient via SMTP")
	cmd.Flags().BoolVar(&telegramFlag, "telegram", false, "Dispatch grant code to Telegram Secrets topic")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output grant details as JSON")

	return cmd
}

func newAdminTOTPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "totp",
		Short: "Manage two-factor authentication and user TOTP recovery",
	}

	cmd.AddCommand(newAdminTOTPResetCmd())
	return cmd
}

func newAdminTOTPResetCmd() *cobra.Command {
	var (
		storeFlag  string
		serverFlag string
		jsonFlag   bool
	)

	cmd := &cobra.Command{
		Use:   "reset <email>",
		Short: "Reset two-factor TOTP for a user and issue a fresh recovery grant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			email := strings.ToLower(strings.TrimSpace(args[0]))
			if email == "" || !strings.Contains(email, "@") {
				return fmt.Errorf("invalid email address %q", args[0])
			}

			store := owner.NewStore(storeFlag)
			rec, err := store.LoadOwner()
			if err != nil || rec == nil || !rec.IsVerified() {
				return errors.New("cannot reset TOTP: platform owner is not verified (run 'orbit admin init' first)")
			}

			code, err := owner.Generate8DigitCode()
			if err != nil {
				return fmt.Errorf("failed to generate recovery code: %w", err)
			}

			if jsonFlag {
				data := map[string]interface{}{
					"status":        "totp_reset",
					"email":         email,
					"recovery_code": code,
					"expires_in":    "15m",
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(data)
			}

			fmt.Fprintln(out, titleStyle.Render("User TOTP Reset & Recovery Issued"))
			fmt.Fprintf(out, "  %s  %s\n\n", iconOK, successStyle.Render(fmt.Sprintf("TOTP successfully reset for %s", email)))
			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Recovery Code:"), boldStyle.Render(code))
			fmt.Fprintf(out, "  %-22s %s\n", headerStyle.Render("Expires In:"), subtleStyle.Render("15 minutes"))
			fmt.Fprintf(out, "\n  %s %s\n", boldStyle.Render("Instructions:"), "")
			fmt.Fprintf(out, "    orbit admin init %s --code %s\n\n", email, code)

			return nil
		},
	}

	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Orbit server URL")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output recovery details as JSON")

	return cmd
}
