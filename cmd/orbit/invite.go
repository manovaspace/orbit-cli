package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/tui/forms"
	"github.com/spf13/cobra"
)

const (
	DefaultSecretEnv      = "ORBIT_INVITE_SECRET"
	DefaultFallbackSecret = "orbit-dev-insecure-invitation-signing-secret-key-32bytes"
)

func newInviteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Manage developer onboarding invite tokens",
		Long: `Create, list, and revoke cryptographically signed developer onboarding invitations.
Invitations are HMAC-SHA256 signed single-use or scoped tokens with built-in expiration
and claims for automated developer onboarding.`,
	}

	cmd.AddCommand(newInviteCreateCmd())
	cmd.AddCommand(newInviteListCmd())
	cmd.AddCommand(newInviteRevokeCmd())

	return cmd
}

func newInviteCreateCmd() *cobra.Command {
	var (
		interactiveFlag bool
		emailFlag       string
		nameFlag        string
		scopeFlag       string
		expiresFlag     string
		secretEnvFlag   string
		secretFlag      string
		storeFileFlag   string
		ownerStoreFlag  string
		insecureFlag    bool
		sendFlag        bool
		noSendFlag      bool
		smtpHostFlag    string
		smtpPortFlag    string
		smtpFromFlag    string
	)

	cmd := &cobra.Command{
		Use:   "create [email]",
		Short: "Generate a cryptographically signed onboarding invitation token",
		Long:  "Generates a HMAC-SHA256 signed invite token for a developer email address with specified scope and expiration.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var email string
			var createAlias bool
			if len(args) > 0 {
				email = strings.TrimSpace(args[0])
			}
			if email == "" && emailFlag != "" {
				email = strings.TrimSpace(emailFlag)
			}

			if interactiveFlag {
				if !istty.IsInteractiveSession() {
					return errors.New("interactive mode requires an active terminal session")
				}
				initialRole := scopeFlag
				initialTTL := expiresFlag
				formData, err := forms.RunInviteForm(email, initialRole, initialTTL)
				if err != nil {
					return err
				}
				email = formData.Email
				scopeFlag = formData.Role
				expiresFlag = formData.TTL
				createAlias = formData.CreateAlias
			} else {
				if email == "" {
					return errors.New("required flag(s) \"email\" not set (use -i for interactive mode)")
				}
			}

			if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
				return fmt.Errorf("invalid email address %q", email)
			}

			ttl, err := parseDuration(expiresFlag)
			if err != nil {
				return err
			}

			// Check platform ownership verification
			ownerStore := owner.NewStore(ownerStoreFlag)
			ownerRecord, ownerErr := ownerStore.LoadOwner()
			isOwnerVerified := (ownerErr == nil && ownerRecord != nil && ownerRecord.IsVerified())

			isBypassed := insecureFlag || os.Getenv("ORBIT_INSECURE_SKIP_OWNER_CHECK") == "true"
			if !isOwnerVerified && !isBypassed {
				return errors.New("platform ownership is unverified. Run 'orbit admin init --owner <email>' to verify ownership before issuing invitations.")
			}

			var verifiedSecret string
			if isOwnerVerified && ownerRecord != nil {
				verifiedSecret = ownerRecord.RootSigningSecret
			}

			secret, isFallback := resolveSecret(secretFlag, secretEnvFlag, verifiedSecret)
			if len(secret) == 0 {
				return errors.New("signing secret cannot be empty")
			}
			if isFallback {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n\n",
					iconWarn,
					warningStyle.Render("Using default dev signing secret. For production tokens, set $MANOVA_INVITE_SECRET or pass --secret."),
				)
			}

			var metadata map[string]string
			if createAlias {
				metadata = map[string]string{
					"create_alias": "true",
				}
			}

			createdBy := ""
			if isOwnerVerified && ownerRecord != nil {
				createdBy = ownerRecord.Email
			}

			req := invite.InviteRequest{
				Email:       email,
				DisplayName: strings.TrimSpace(nameFlag),
				Scope:       strings.TrimSpace(scopeFlag),
				TTL:         ttl,
				CreatedBy:   createdBy,
				Metadata:    metadata,
			}
			if req.Scope == "" {
				req.Scope = "core"
			}

			tokenStr, claims, err := invite.GenerateToken(req, secret)
			if err != nil {
				return fmt.Errorf("failed to generate invite token: %w", err)
			}

			if createAlias {
				if claims.Metadata == nil {
					claims.Metadata = make(map[string]string)
				}
				claims.Metadata["create_alias"] = "true"
			}

			// Save invite to store
			store, err := invite.NewStore(storeFileFlag)
			if err != nil {
				return fmt.Errorf("failed to initialize invite store: %w", err)
			}

			rec := &invite.InviteRecord{
				ID:          claims.ID,
				Email:       claims.Email,
				DisplayName: claims.DisplayName,
				Scope:       claims.Scope,
				Token:       tokenStr,
				Revoked:     false,
				IssuedAt:    claims.IssuedAt,
				ExpiresAt:   claims.ExpiresAt,
				CreatedBy:   claims.CreatedBy,
				Metadata:    claims.Metadata,
			}

			if err := store.SaveInvite(rec); err != nil {
				return fmt.Errorf("failed to save invitation record: %w", err)
			}

			shortCode := claims.ID
			if len(shortCode) >= 6 {
				shortCode = strings.ToUpper(shortCode[:3] + "-" + shortCode[3:6])
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Orbit Developer Invitation Generated"))
			fmt.Fprintf(out, "  %s  Signed invitation token created for %s\n",
				iconOK,
				boldStyle.Render(email),
			)
			fmt.Fprintf(out, "  %s  Web Setup: %s\n", iconOK, infoStyle.Render(fmt.Sprintf("https://orbit.manova.space/setup?token=%s", tokenStr)))

			// Dispatch email by default unless suppressed
			if sendFlag && !noSendFlag {
				mailerCfg := invite.MailerConfig{
					Host: smtpHostFlag,
					Port: smtpPortFlag,
					From: smtpFromFlag,
				}
				if mailerCfg.Host == "" {
					mailerCfg.Host = os.Getenv("ORBIT_SMTP_HOST")
					if mailerCfg.Host == "" {
						if insecureFlag {
							mailerCfg.Host = "localhost"
						} else {
							mailerCfg.Host = "mail.manova.space"
						}
					}
				}
				if mailerCfg.Port == "" {
					mailerCfg.Port = os.Getenv("ORBIT_SMTP_PORT")
					if mailerCfg.Port == "" {
						if insecureFlag {
							mailerCfg.Port = "10725"
						} else {
							mailerCfg.Port = "587"
						}
					}
				}
				if mailerCfg.From == "" {
					mailerCfg.From = os.Getenv("ORBIT_SMTP_FROM")
					if mailerCfg.From == "" {
						mailerCfg.From = "Orbit Platform <noreply@manova.space>"
					}
				}
				mailerCfg.User = os.Getenv("ORBIT_SMTP_USER")
				mailerCfg.Pass = os.Getenv("ORBIT_SMTP_PASS")

				mailer := invite.NewSMTPMailer(mailerCfg)
				emailData := invite.EmailData{
					RecipientName:  claims.DisplayName,
					RecipientEmail: claims.Email,
					Token:          tokenStr,
					ShortCode:      tokenStr,
					Scope:          claims.Scope,
					ExpiresAt:      claims.ExpiresAt,
					ExpiresInHuman: invite.FormatRemaining(time.Until(claims.ExpiresAt)),
					CLICommand:     fmt.Sprintf("orbit onboard --token %s", tokenStr),
					CurlCommand:    fmt.Sprintf("curl -fsSL https://orbit.manova.space | bash -s -- onboard --token %s", tokenStr),
				}

				if sendErr := mailer.SendInvite(cmd.Context(), claims.Email, emailData); sendErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %s  %s: %v\n", iconWarn, warningStyle.Render("Failed to dispatch invitation email"), sendErr)
				} else {
					fmt.Fprintf(out, "  %s  Invitation email dispatched to %s via %s:%s\n",
						iconOK,
						boldStyle.Render(claims.Email),
						codeStyle.Render(mailerCfg.Host),
						codeStyle.Render(mailerCfg.Port),
					)
				}
			}
			fmt.Fprintln(out)

			fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Token:"), boldStyle.Render(tokenStr))
			fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Email:"), boldStyle.Render(claims.Email))
			if claims.DisplayName != "" {
				fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Name:"), claims.DisplayName)
			}
			if claims.CreatedBy != "" {
				fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Created By:"), boldStyle.Render(claims.CreatedBy))
			}
			fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Scope:"), infoStyle.Render(claims.Scope))
			fmt.Fprintf(out, "  %-14s %s (%s)\n\n",
				headerStyle.Render("Expires:"),
				claims.ExpiresAt.Format("2006-01-02 15:04:05 UTC"),
				warningStyle.Render(formatRemaining(time.Until(claims.ExpiresAt))),
			)

			instructions := fmt.Sprintf(
				"To onboard with this invitation token, run:\n\n"+
					"  %s\n\n"+
					"Or using shortcut:\n\n"+
					"  %s",
				codeStyle.Render("orbit onboard --token "+tokenStr),
				codeStyle.Render("o onboard --token "+tokenStr),
			)
			fmt.Fprintln(out, renderCard("ONBOARDING INSTRUCTIONS", instructions))

			return nil
		},
	}

	cmd.Flags().BoolVarP(&interactiveFlag, "interactive", "i", false, "Interactive wizard for generating developer invitations")
	cmd.Flags().StringVar(&emailFlag, "email", "", "Developer email address")
	cmd.Flags().StringVarP(&nameFlag, "name", "n", "", "Display name of the developer (e.g. 'Alex Smith')")
	cmd.Flags().StringVarP(&scopeFlag, "scope", "s", "core", "Access scope (e.g. 'core', 'client', 'guest')")
	cmd.Flags().StringVarP(&expiresFlag, "expires", "e", "168h", "Expiration duration (e.g. '7d', '24h', '168h')")
	cmd.Flags().StringVar(&secretEnvFlag, "secret-env", DefaultSecretEnv, "Environment variable containing signing secret")
	cmd.Flags().StringVar(&secretFlag, "secret", "", "Raw signing secret (overrides --secret-env)")
	cmd.Flags().StringVar(&storeFileFlag, "store-file", "", "Custom path to invites storage file")
	cmd.Flags().StringVar(&ownerStoreFlag, "owner-store", "", "Custom path to owner storage vault file (default: $ORBIT_OWNER_STORE or ~/.config/orbit/owner.json)")
	cmd.Flags().BoolVar(&insecureFlag, "insecure", false, "Bypass owner verification check (development only)")
	cmd.Flags().BoolVarP(&sendFlag, "send", "m", true, "Dispatch onboarding invitation email via SMTP (default: true)")
	cmd.Flags().BoolVar(&noSendFlag, "no-send", false, "Suppress dispatching onboarding invitation email")
	cmd.Flags().StringVar(&smtpHostFlag, "smtp-host", "", "SMTP server host (default: $ORBIT_SMTP_HOST or mail.manova.space)")
	cmd.Flags().StringVar(&smtpPortFlag, "smtp-port", "", "SMTP server port (default: $ORBIT_SMTP_PORT or 587)")
	cmd.Flags().StringVar(&smtpFromFlag, "smtp-from", "", "Sender email address (default: $ORBIT_SMTP_FROM)")

	return cmd
}

func newInviteListCmd() *cobra.Command {
	var (
		formatFlag    string
		storeFileFlag string
		allFlag       bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active and stored developer onboarding invitations",
		Long:  "Displays a table or JSON array of stored invitations with their status, scope, and expiration. By default, only active invitations are shown; use --all to include revoked and expired.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			store, err := invite.NewStore(storeFileFlag)
			if err != nil {
				return fmt.Errorf("failed to initialize invite store: %w", err)
			}

			allRecords, err := store.ListInvites()
			if err != nil {
				return fmt.Errorf("failed to list invitations: %w", err)
			}

			if len(allRecords) == 0 {
				if strings.ToLower(formatFlag) == "json" {
					fmt.Fprintln(out, "[]")
					return nil
				}
				fmt.Fprintln(out, infoStyle.Render("No invitations found. Run 'orbit invite create <email>' to generate one."))
				return nil
			}

			var displayRecords []*invite.InviteRecord
			activeCount := 0
			revokedCount := 0
			expiredCount := 0

			for _, r := range allRecords {
				status := r.Status()
				switch status {
				case "active":
					activeCount++
				case "revoked":
					revokedCount++
				case "expired":
					expiredCount++
				}

				if allFlag || status == "active" {
					displayRecords = append(displayRecords, r)
				}
			}

			if strings.ToLower(formatFlag) == "json" {
				if displayRecords == nil {
					displayRecords = []*invite.InviteRecord{}
				}
				data, err := json.MarshalIndent(displayRecords, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON output: %w", err)
				}
				fmt.Fprintln(out, string(data))
				return nil
			}

			if len(displayRecords) == 0 {
				fmt.Fprintln(out, infoStyle.Render("No active invitations found. Use --all (-a) to view expired or revoked invitations, or 'orbit invite create <email>' to generate one."))
				return nil
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit Developer Invitations"))

			// Table header
			fmt.Fprintf(out, "\n  %-18s %-26s %-16s %-10s %-14s %-22s %s\n",
				headerStyle.Render("INVITE ID"),
				headerStyle.Render("EMAIL"),
				headerStyle.Render("NAME"),
				headerStyle.Render("SCOPE"),
				headerStyle.Render("STATUS"),
				headerStyle.Render("EXPIRES"),
				headerStyle.Render("CREATED"),
			)
			fmt.Fprintln(out, subtleStyle.Render("  ─────────────────────────────────────────────────────────────────────────────────────────────────────────────"))

			for _, r := range displayRecords {
				status := r.Status()
				var statusBadge string
				switch status {
				case "active":
					statusBadge = successStyle.Render("✔ active")
				case "revoked":
					statusBadge = errorStyle.Render("✖ revoked")
				case "expired":
					statusBadge = warningStyle.Render("⚠ expired")
				default:
					statusBadge = subtleStyle.Render(status)
				}

				nameCol := r.DisplayName
				if nameCol == "" {
					nameCol = "-"
				}

				idCol := r.ID
				if len(idCol) > 16 {
					idCol = idCol[:16] + "…"
				}

				fmt.Fprintf(out, "  %-18s %-26s %-16s %-10s %-14s %-22s %s\n",
					codeStyle.Render(padRight(idCol, 18)),
					boldStyle.Render(padRight(r.Email, 26)),
					subtleStyle.Render(padRight(nameCol, 16)),
					infoStyle.Render(padRight(r.Scope, 10)),
					padRight(statusBadge, 14),
					subtleStyle.Render(padRight(r.ExpiresAt.Format("2006-01-02 15:04 UTC"), 22)),
					subtleStyle.Render(r.IssuedAt.Format("2006-01-02 15:04 UTC")),
				)
			}

			fmt.Fprintf(out, "\n%s  %s  %s  %s\n",
				infoStyle.Render(fmt.Sprintf("Total: %d", len(allRecords))),
				successStyle.Render(fmt.Sprintf("✔ %d active", activeCount)),
				errorStyle.Render(fmt.Sprintf("✖ %d revoked", revokedCount)),
				warningStyle.Render(fmt.Sprintf("⚠ %d expired", expiredCount)),
			)

			return nil
		},
	}

	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format (table or json)")
	cmd.Flags().StringVar(&storeFileFlag, "store-file", "", "Custom path to invites storage file")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Include revoked and expired invitations")

	return cmd
}

func newInviteRevokeCmd() *cobra.Command {
	var (
		storeFileFlag string
		allFlag       bool
	)

	cmd := &cobra.Command{
		Use:   "revoke [token_or_id]",
		Short: "Revoke active developer onboarding invitations",
		Long:  "Marks an invitation token or all active invitations as revoked, preventing further claim attempts.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			store, err := invite.NewStore(storeFileFlag)
			if err != nil {
				return fmt.Errorf("failed to initialize invite store: %w", err)
			}

			if allFlag {
				if len(args) > 0 {
					return errors.New("cannot specify positional invite token/ID when --all is used")
				}

				revoked, err := store.RevokeAllInvites()
				if err != nil {
					return fmt.Errorf("failed to revoke all invitations: %w", err)
				}

				if len(revoked) == 0 {
					fmt.Fprintln(out, infoStyle.Render("No active invitations to revoke."))
					return nil
				}

				fmt.Fprintln(out, titleStyle.Render("Invitations Revoked"))
				fmt.Fprintf(out, "  %s  Revoked %d active invitation(s):\n",
					iconOK,
					len(revoked),
				)
				for _, r := range revoked {
					idCol := r.ID
					if len(idCol) > 16 {
						idCol = idCol[:16] + "…"
					}
					fmt.Fprintf(out, "    • %s  %-26s (scope: %s)\n",
						codeStyle.Render(padRight(idCol, 18)),
						boldStyle.Render(r.Email),
						infoStyle.Render(r.Scope),
					)
				}
				return nil
			}

			if len(args) == 0 {
				return errors.New("invite token or ID required (or use --all to revoke all active invitations)")
			}
			if len(args) > 1 {
				return errors.New("accepts at most 1 arg(s), received multiple")
			}

			tokenOrID := strings.TrimSpace(args[0])
			if tokenOrID == "" {
				return errors.New("invite token or ID required (or use --all to revoke all active invitations)")
			}

			rec, err := store.RevokeInvite(tokenOrID)
			if err != nil {
				if errors.Is(err, invite.ErrInviteNotFound) {
					return fmt.Errorf("invitation %q not found in store", tokenOrID)
				}
				return fmt.Errorf("failed to revoke invitation: %w", err)
			}

			fmt.Fprintln(out, titleStyle.Render("Invitation Revoked"))
			fmt.Fprintf(out, "  %s  Revoked invitation %s for %s (scope: %s)\n",
				iconOK,
				codeStyle.Render(rec.ID),
				boldStyle.Render(rec.Email),
				infoStyle.Render(rec.Scope),
			)

			return nil
		},
	}

	cmd.Flags().StringVar(&storeFileFlag, "store-file", "", "Custom path to invites storage file")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Revoke all active developer onboarding invitations")

	return cmd
}

func resolveSecret(secretFlag, secretEnvFlag, verifiedOwnerSecret string) ([]byte, bool) {
	if secretFlag != "" {
		return []byte(secretFlag), false
	}

	envVar := secretEnvFlag
	if envVar == "" {
		envVar = DefaultSecretEnv
	}

	if val := os.Getenv(envVar); val != "" {
		return []byte(val), false
	}

	if val := os.Getenv("MANOVA_INVITE_SECRET"); val != "" {
		return []byte(val), false
	}

	if jwtVal := os.Getenv("ORBIT_JWT_SECRET"); jwtVal != "" {
		return []byte(jwtVal), false
	}

	if jwtVal := os.Getenv("MANOVA_JWT_SECRET"); jwtVal != "" {
		return []byte(jwtVal), false
	}

	if verifiedOwnerSecret != "" {
		return []byte(verifiedOwnerSecret), false
	}

	return []byte(DefaultFallbackSecret), true
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 7 * 24 * time.Hour, nil
	}

	// Handle day suffix "d" or "D", e.g. "7d", "14d", "1d"
	if strings.HasSuffix(s, "d") || strings.HasSuffix(s, "D") {
		daysStr := s[:len(s)-1]
		days, err := strconv.Atoi(daysStr)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid day duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration format %q (e.g., '7d', '24h', '168h'): %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be greater than zero, got %v", d)
	}
	return d, nil
}

func formatRemaining(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	if d > 24*time.Hour {
		days := int(d.Hours() / 24)
		hours := int(d.Hours()) % 24
		if hours == 0 {
			return fmt.Sprintf("%dd remaining", days)
		}
		return fmt.Sprintf("%dd %dh remaining", days, hours)
	}
	if d > time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm remaining", hours, mins)
	}
	return fmt.Sprintf("%dm remaining", int(d.Minutes()))
}
