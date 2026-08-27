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
		configFlag string
	)

	cmd := &cobra.Command{
		Use:   "init [email]",
		Short: "Initialize and verify platform server ownership via email OTP challenge",
		Long: `Initiates server ownership verification. Dispatches a 6-digit OTP challenge to the
owner's email address via Mailcow, prompts for verification, and seals a 32-byte cryptographic
master signing secret in the local owner vault (mode 0600).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			cfg, err := config.Resolve(config.ResolveOptions{
				ConfigPath: configFlag,
				ServerFlag: serverFlag,
				OwnerFlag:  ownerFlag,
				NameFlag:   nameFlag,
			})
			if err != nil {
				return fmt.Errorf("failed to resolve configuration: %w", err)
			}

			email := strings.TrimSpace(ownerFlag)
			if len(args) > 0 && email == "" {
				email = strings.TrimSpace(args[0])
			}

			if email == "" {
				email = promptString(in, out, "Enter platform owner email address", cfg.Admin.Email)
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
				fmt.Fprintf(cmd.ErrOrStderr(), "  When bypassing email delivery for testing, pass: manova admin init --no-send --code <code>\n\n")
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
				serverURL := strings.TrimSpace(cfg.Server.URL)
				if serverURL == "" {
					serverURL = config.DefaultServerURL
				}
				apiClient = client.NewClient(serverURL)
				fmt.Fprintf(out, "  %s  Connecting to Orbit server at %s...\n", iconArrow, codeStyle.Render(serverURL))
				_, err := apiClient.InitiateOwnerChallenge(cmd.Context(), email)
				if err != nil {
					return fmt.Errorf("failed to initiate challenge on server %s: %w", serverURL, err)
				}
				fmt.Fprintf(out, "  %s  Verification challenge dispatched to %s via Mailcow\n",
					iconOK,
					boldStyle.Render(email),
				)
			}

			// Acquire code from user if not passed via --code
			inputCode := strings.TrimSpace(codeFlag)
			if inputCode == "" {
				inputCode = promptString(in, out, "Enter 6-digit verification code", "")
				inputCode = strings.TrimSpace(inputCode)
			}

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
					return fmt.Errorf("server rejected OTP verification (status: %s)", vResp.Status)
				}
			}

			// Generate 32-byte master signing secret
			secret, err := owner.GenerateMasterSecret()
			if err != nil {
				return fmt.Errorf("failed to generate master signing secret: %w", err)
			}

			displayName := strings.TrimSpace(nameFlag)
			if displayName == "" {
				displayName = cfg.Admin.Name
			}
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

	cmd.Flags().StringVarP(&ownerFlag, "owner", "o", "", "Owner email address (e.g. alirezaopmc@gmail.com)")
	cmd.Flags().StringVarP(&nameFlag, "name", "n", "", "Owner display name (e.g. 'Alireza')")
	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Orbit server URL (e.g. https://orbit.manova.space)")
	cmd.Flags().BoolVar(&noSendFlag, "no-send", false, "Suppress dispatching challenge email")
	cmd.Flags().StringVarP(&codeFlag, "code", "c", "", "6-digit verification code (for non-interactive execution)")
	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force re-initialization even if already verified")
	cmd.Flags().StringVar(&configFlag, "config", "", "Custom path to configuration file")

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
		configFlag string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display platform ownership verification status, vault integrity, and mail config",
		Long:  "Reports whether platform ownership has been verified, vault file path and permissions, and active Mailcow gateway.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			store := owner.NewStore(storeFlag)
			rec, err := store.LoadOwner()
			permErr := store.CheckPermissions()

			cfg, _ := config.Resolve(config.ResolveOptions{
				ConfigPath: configFlag,
			})

			mailHost := cfg.SMTP.Host
			if cfg.SMTP.Port > 0 && !strings.Contains(mailHost, ":") {
				mailHost = fmt.Sprintf("%s:%d", cfg.SMTP.Host, cfg.SMTP.Port)
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
	cmd.Flags().StringVar(&configFlag, "config", "", "Custom path to configuration file")

	return cmd
}

func newAdminVerifyCmd() *cobra.Command {
	var (
		ownerFlag  string
		codeFlag   string
		nameFlag   string
		storeFlag  string
		forceFlag  bool
		configFlag string
	)

	cmd := &cobra.Command{
		Use:   "verify [email] [code]",
		Short: "Complete owner verification using a received 6-digit OTP code",
		Long:  "Verifies a 6-digit OTP challenge, generates a cryptographic master signing secret, and seals the owner record in the vault.",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			cfg, _ := config.Resolve(config.ResolveOptions{
				ConfigPath: configFlag,
				OwnerFlag:  ownerFlag,
				NameFlag:   nameFlag,
			})

			email := strings.TrimSpace(ownerFlag)
			if len(args) > 0 && email == "" {
				email = strings.TrimSpace(args[0])
			}
			if email == "" {
				email = promptString(in, out, "Enter owner email address", cfg.Admin.Email)
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
			if displayName == "" {
				displayName = cfg.Admin.Name
			}
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
	cmd.Flags().StringVar(&configFlag, "config", "", "Custom path to configuration file")

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
