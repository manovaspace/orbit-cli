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
		nameFlag      string
		scopeFlag     string
		expiresFlag   string
		secretEnvFlag string
		secretFlag    string
		storeFileFlag string
	)

	cmd := &cobra.Command{
		Use:   "create <email>",
		Short: "Generate a cryptographically signed onboarding invitation token",
		Long:  "Generates a HMAC-SHA256 signed invite token for a developer email address with specified scope and expiration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := strings.TrimSpace(args[0])
			if email == "" || !strings.Contains(email, "@") || !strings.Contains(email, ".") {
				return fmt.Errorf("invalid email address %q", email)
			}

			ttl, err := parseDuration(expiresFlag)
			if err != nil {
				return err
			}

			secret, isFallback := resolveSecret(secretFlag, secretEnvFlag)
			if len(secret) == 0 {
				return errors.New("signing secret cannot be empty")
			}
			if isFallback {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n\n",
					iconWarn,
					warningStyle.Render("Using default dev signing secret. For production tokens, set $MANOVA_INVITE_SECRET or pass --secret."),
				)
			}

			req := invite.InviteRequest{
				Email:       email,
				DisplayName: strings.TrimSpace(nameFlag),
				Scope:       strings.TrimSpace(scopeFlag),
				TTL:         ttl,
			}
			if req.Scope == "" {
				req.Scope = "core"
			}

			tokenStr, claims, err := invite.GenerateToken(req, secret)
			if err != nil {
				return fmt.Errorf("failed to generate invite token: %w", err)
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

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Orbit Developer Invitation Generated"))
			fmt.Fprintf(out, "  %s  Signed invitation token created for %s\n\n",
				iconOK,
				boldStyle.Render(email),
			)

			fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Invite ID:"), codeStyle.Render(claims.ID))
			fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Email:"), boldStyle.Render(claims.Email))
			if claims.DisplayName != "" {
				fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Name:"), claims.DisplayName)
			}
			fmt.Fprintf(out, "  %-14s %s\n", headerStyle.Render("Scope:"), infoStyle.Render(claims.Scope))
			fmt.Fprintf(out, "  %-14s %s (%s)\n",
				headerStyle.Render("Expires:"),
				claims.ExpiresAt.Format("2006-01-02 15:04:05 UTC"),
				warningStyle.Render(formatRemaining(time.Until(claims.ExpiresAt))),
			)
			fmt.Fprintf(out, "  %-14s %s\n\n", headerStyle.Render("Token:"), codeStyle.Render(tokenStr))

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

	cmd.Flags().StringVarP(&nameFlag, "name", "n", "", "Display name of the developer (e.g. 'Alex Smith')")
	cmd.Flags().StringVarP(&scopeFlag, "scope", "s", "core", "Access scope (e.g. 'core', 'client', 'guest')")
	cmd.Flags().StringVarP(&expiresFlag, "expires", "e", "168h", "Expiration duration (e.g. '7d', '24h', '168h')")
	cmd.Flags().StringVar(&secretEnvFlag, "secret-env", DefaultSecretEnv, "Environment variable containing signing secret")
	cmd.Flags().StringVar(&secretFlag, "secret", "", "Raw signing secret (overrides --secret-env)")
	cmd.Flags().StringVar(&storeFileFlag, "store-file", "", "Custom path to invites storage file")

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
		Long:  "Displays a table or JSON array of all stored invitations with their status, scope, and expiration.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			store, err := invite.NewStore(storeFileFlag)
			if err != nil {
				return fmt.Errorf("failed to initialize invite store: %w", err)
			}

			records, err := store.ListInvites()
			if err != nil {
				return fmt.Errorf("failed to list invitations: %w", err)
			}

			if strings.ToLower(formatFlag) == "json" {
				if records == nil {
					records = []*invite.InviteRecord{}
				}
				data, err := json.MarshalIndent(records, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON output: %w", err)
				}
				fmt.Fprintln(out, string(data))
				return nil
			}

			if len(records) == 0 {
				fmt.Fprintln(out, infoStyle.Render("No invitations found. Run 'orbit invite create <email>' to generate one."))
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

			activeCount := 0
			revokedCount := 0
			expiredCount := 0

			for _, r := range records {
				status := r.Status()
				var statusBadge string
				switch status {
				case "active":
					activeCount++
					statusBadge = successStyle.Render("✔ active")
				case "revoked":
					revokedCount++
					statusBadge = errorStyle.Render("✖ revoked")
				case "expired":
					expiredCount++
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
				infoStyle.Render(fmt.Sprintf("Total: %d", len(records))),
				successStyle.Render(fmt.Sprintf("✔ %d active", activeCount)),
				errorStyle.Render(fmt.Sprintf("✖ %d revoked", revokedCount)),
				warningStyle.Render(fmt.Sprintf("⚠ %d expired", expiredCount)),
			)

			return nil
		},
	}

	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format (table or json)")
	cmd.Flags().StringVar(&storeFileFlag, "store-file", "", "Custom path to invites storage file")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", true, "Include revoked and expired invitations")

	return cmd
}

func newInviteRevokeCmd() *cobra.Command {
	var storeFileFlag string

	cmd := &cobra.Command{
		Use:   "revoke <token_or_id>",
		Short: "Revoke an active developer onboarding invitation",
		Long:  "Marks an invitation token as revoked by its token string or invite ID, preventing further claim attempts.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenOrID := strings.TrimSpace(args[0])
			if tokenOrID == "" {
				return errors.New("invite token or ID required")
			}

			store, err := invite.NewStore(storeFileFlag)
			if err != nil {
				return fmt.Errorf("failed to initialize invite store: %w", err)
			}

			rec, err := store.RevokeInvite(tokenOrID)
			if err != nil {
				if errors.Is(err, invite.ErrInviteNotFound) {
					return fmt.Errorf("invitation %q not found in store", tokenOrID)
				}
				return fmt.Errorf("failed to revoke invitation: %w", err)
			}

			out := cmd.OutOrStdout()
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

	return cmd
}

func resolveSecret(secretFlag, secretEnvFlag string) ([]byte, bool) {
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
