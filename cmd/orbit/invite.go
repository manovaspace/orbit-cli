package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	inviteapp "github.com/manovaspace/orbit-cli/pkg/invite/application"
	invitedomain "github.com/manovaspace/orbit-cli/pkg/invite/domain"
	invitefilestore "github.com/manovaspace/orbit-cli/pkg/invite/infrastructure/filestore"
	"github.com/manovaspace/orbit-cli/pkg/invite/infrastructure/ownerstore"
	invitesmtp "github.com/manovaspace/orbit-cli/pkg/invite/infrastructure/smtp"
	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/manovaspace/orbit-cli/pkg/output"
	ownerfilestore "github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/filestore"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/manovaspace/orbit-cli/pkg/tui/forms"
	"github.com/spf13/cobra"
)

const (
	DefaultSecretEnv      = invitedomain.DefaultSecretEnv
	DefaultFallbackSecret = invitedomain.DefaultFallbackSecret
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

// inviteService builds the invitation use-case layer from the command flags.
func inviteService(cmd *cobra.Command, storeFile, ownerStore string) (*inviteapp.Service, error) {
	store, err := invitefilestore.NewStore(storeFile)
	if err != nil {
		return nil, err
	}
	return &inviteapp.Service{
		Store:     store,
		Owner:     ownerstore.New(ownerfilestore.NewStore(ownerStore)),
		LookupEnv: lookupEnv,
		Now:       func() time.Time { return time.Now().UTC() },
		Out:       cmd.OutOrStdout(),
		Err:       cmd.ErrOrStderr(),
		UI: inviteapp.UI{
			Warn:    iconWarn,
			Warning: func(text string) string { return warningStyle.Render(text) },
		},
	}, nil
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
		totpFlag        bool
		smtpHostFlag    string
		smtpPortFlag    string
		smtpFromFlag    string
		jsonFlag        bool
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
					err := errors.New("interactive mode requires an active terminal session")
					identityWriteFailure(cmd.OutOrStdout(), jsonFlag, err)
					return err
				}
				initialRole := scopeFlag
				initialTTL := expiresFlag
				formData, err := forms.RunInviteForm(email, initialRole, initialTTL)
				if err != nil {
					identityWriteFailure(cmd.OutOrStdout(), jsonFlag, err)
					return err
				}
				email = formData.Email
				scopeFlag = formData.Role
				expiresFlag = formData.TTL
				createAlias = formData.CreateAlias
			}

			svc, err := inviteService(cmd, storeFileFlag, ownerStoreFlag)
			if err != nil {
				err = fmt.Errorf("failed to initialize invite store: %w", err)
				identityWriteFailure(cmd.OutOrStdout(), jsonFlag, err)
				return err
			}

			var mailer inviteapp.Mailer
			mailerCfg := inviteMailerConfig(smtpHostFlag, smtpPortFlag, smtpFromFlag, insecureFlag)
			send := sendFlag && !noSendFlag
			if send {
				mailer = invitesmtp.NewSMTPMailer(mailerCfg)
			}
			if jsonFlag {
				svc.Out = nil
				svc.Err = nil
			}

			res, err := svc.Create(cmd.Context(), inviteapp.CreateInput{
				Email:       email,
				DisplayName: nameFlag,
				Scope:       scopeFlag,
				Expires:     expiresFlag,
				SecretFlag:  secretFlag,
				SecretEnv:   secretEnvFlag,
				Insecure:    insecureFlag,
				Alias:       createAlias,
				TOTP:        totpFlag,
				Send:        send,
				Mailer:      mailer,
				MailHost:    mailerCfg.Host,
				MailPort:    mailerCfg.Port,
			})
			if err != nil {
				identityWriteFailure(cmd.OutOrStdout(), jsonFlag, err)
				return err
			}

			if jsonFlag {
				return output.Write(cmd.OutOrStdout(), output.Success(res))
			}

			out := cmd.OutOrStdout()
			p := render.NewPrinter(out)
			p.Title("Orbit Developer Invitation Generated")
			p.Line(render.StatusOK, "Signed invitation token created for "+render.Bold.Render(res.Email))
			p.Line(render.StatusOK, "Web Setup: "+render.Info.Render("https://orbit.manova.space/setup?token="+res.Token))

			if send {
				if res.MailError != "" {
					render.NewPrinter(cmd.ErrOrStderr()).Line(render.StatusWarn, render.Warning.Render("Failed to dispatch invitation email")+": "+res.MailError)
				} else {
					p.Line(render.StatusOK, fmt.Sprintf("Invitation email dispatched to %s via %s:%s",
						render.Bold.Render(res.Email),
						render.Code.Render(mailerCfg.Host),
						render.Code.Render(mailerCfg.Port),
					))
				}
			}
			p.Blank()

			pairs := [][2]string{
				{"Token:", render.Bold.Render(res.Token)},
				{"Email:", render.Bold.Render(res.Email)},
			}
			if res.DisplayName != "" {
				pairs = append(pairs, [2]string{"Name:", res.DisplayName})
			}
			if res.CreatedBy != "" {
				pairs = append(pairs, [2]string{"Created By:", render.Bold.Render(res.CreatedBy)})
			}
			pairs = append(pairs, [2]string{"Scope:", render.Info.Render(res.Scope)})
			if res.TOTPSecret != "" {
				pairs = append(pairs,
					[2]string{"TOTP Secret:", render.Code.Render(res.TOTPSecret)},
					[2]string{"TOTP URI:", render.Subtle.Render(res.TOTPURI)},
				)
			}
			pairs = append(pairs, [2]string{"Expires:", res.ExpiresAt.Format("2006-01-02 15:04:05 UTC") + " (" + render.Warning.Render(invitedomain.FormatRemainingLabel(time.Until(res.ExpiresAt))) + ")"})
			p.KVs(pairs...)
			p.Blank()

			instructions := fmt.Sprintf(
				"To onboard with this invitation token, run:\n\n"+
					"  %s\n\n"+
					"Or using shortcut:\n\n"+
					"  %s",
				render.Code.Render("orbit onboard --token "+res.Token),
				render.Code.Render("o onboard --token "+res.Token),
			)
			p.Text(renderCard("ONBOARDING INSTRUCTIONS", instructions) + "\n")

			return p.Err()
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
	cmd.Flags().BoolVar(&totpFlag, "totp", false, "Generate Authelia-compatible 2FA TOTP secret and embed QR code in setup link")
	cmd.Flags().StringVar(&smtpHostFlag, "smtp-host", "", "SMTP server host (default: $ORBIT_SMTP_HOST or mail.manova.space)")
	cmd.Flags().StringVar(&smtpPortFlag, "smtp-port", "", "SMTP server port (default: $ORBIT_SMTP_PORT or 587)")
	cmd.Flags().StringVar(&smtpFromFlag, "smtp-from", "", "Sender email address (default: $ORBIT_SMTP_FROM)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

// inviteMailerConfig resolves the SMTP settings from flags, environment and defaults.
func inviteMailerConfig(hostFlag, portFlag, fromFlag string, insecure bool) invitesmtp.MailerConfig {
	cfg := invitesmtp.MailerConfig{Host: hostFlag, Port: portFlag, From: fromFlag}
	if cfg.Host == "" {
		cfg.Host = os.Getenv("ORBIT_SMTP_HOST")
		if cfg.Host == "" {
			if insecure {
				cfg.Host = "localhost"
			} else {
				cfg.Host = "mail.manova.space"
			}
		}
	}
	if cfg.Port == "" {
		cfg.Port = os.Getenv("ORBIT_SMTP_PORT")
		if cfg.Port == "" {
			if insecure {
				cfg.Port = "10024"
			} else {
				cfg.Port = "587"
			}
		}
	}
	if cfg.From == "" {
		cfg.From = os.Getenv("ORBIT_SMTP_FROM")
		if cfg.From == "" {
			cfg.From = "Orbit Platform <noreply@manova.space>"
		}
	}
	cfg.User = os.Getenv("ORBIT_SMTP_USER")
	cfg.Pass = os.Getenv("ORBIT_SMTP_PASS")
	return cfg
}

func newInviteListCmd() *cobra.Command {
	var (
		formatFlag    string
		storeFileFlag string
		allFlag       bool
		pageFlag      int
		limitFlag     int
		jsonFlag      bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active and stored developer onboarding invitations",
		Long:  "Displays a table or JSON array of stored invitations with their status, scope, and expiration. By default, only active invitations are shown; use --all to include revoked and expired.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			asJSON := jsonFlag || strings.ToLower(formatFlag) == "json"

			svc, err := inviteService(cmd, storeFileFlag, "")
			if err != nil {
				err = fmt.Errorf("failed to initialize invite store: %w", err)
				identityWriteFailure(out, asJSON, err)
				return err
			}
			if asJSON {
				svc.Out = nil
				svc.Err = nil
			}

			res, err := svc.List(allFlag)
			if err != nil {
				identityWriteFailure(out, asJSON, err)
				return err
			}

			if asJSON {
				displayed := res.Displayed
				if displayed == nil {
					displayed = []*invitedomain.InviteRecord{}
				}
				return output.Write(out, output.Success(displayed))
			}

			p := render.NewPrinter(out)
			if res.Total == 0 {
				p.Line(render.StatusInfo, "No invitations found. Run 'orbit invite create <email>' to generate one.")
				return p.Err()
			}

			if len(res.Displayed) == 0 {
				p.Line(render.StatusInfo, "No active invitations found. Use --all (-a) to view expired or revoked invitations, or 'orbit invite create <email>' to generate one.")
				return p.Err()
			}

			p.Title("Orbit Developer Invitations")

			tbl := table.New(
				table.Column{Title: "INVITE ID", HeaderStyle: headerStyle, CellStyle: codeStyle, MinWidth: 12},
				table.Column{Title: "EMAIL", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 18, Flexible: true},
				table.Column{Title: "NAME", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 10, Flexible: true},
				table.Column{Title: "SCOPE", HeaderStyle: headerStyle, CellStyle: infoStyle, MinWidth: 8},
				table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 10},
				table.Column{Title: "EXPIRES", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 18},
				table.Column{Title: "CREATED", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 18},
			)

			for _, r := range res.Displayed {
				status := r.Status()
				var statusCell table.Cell
				switch status {
				case "active":
					statusCell = table.StyleCell("✔ active", successStyle)
				case "revoked":
					statusCell = table.StyleCell("✖ revoked", errorStyle)
				case "expired":
					statusCell = table.StyleCell("⚠ expired", warningStyle)
				default:
					statusCell = table.StyleCell(status, subtleStyle)
				}

				nameVal := r.DisplayName
				if nameVal == "" {
					nameVal = "-"
				}

				idVal := r.ID
				if len(idVal) > 16 {
					idVal = idVal[:16] + "…"
				}

				tbl.AddStyledRow(
					table.PlainCell(idVal),
					table.PlainCell(r.Email),
					table.PlainCell(nameVal),
					table.PlainCell(r.Scope),
					statusCell,
					table.PlainCell(r.ExpiresAt.Format("2006-01-02 15:04 UTC")),
					table.PlainCell(r.IssuedAt.Format("2006-01-02 15:04 UTC")),
				)
			}

			if limitFlag > 0 {
				tbl.WithPagination(pageFlag, limitFlag)
			}

			if err := p.Table(tbl); err != nil {
				return err
			}

			p.SummaryText(fmt.Sprintf("Total: %d  %d active  %d revoked  %d expired", res.Total, res.Active, res.Revoked, res.Expired))

			return p.Err()
		},
	}

	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format (table or json)")
	cmd.Flags().StringVar(&storeFileFlag, "store-file", "", "Custom path to invites storage file")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Include revoked and expired invitations")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum rows per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

func newInviteRevokeCmd() *cobra.Command {
	var (
		storeFileFlag string
		allFlag       bool
		jsonFlag      bool
	)

	cmd := &cobra.Command{
		Use:   "revoke [token_or_id]",
		Short: "Revoke active developer onboarding invitations",
		Long:  "Marks an invitation token or all active invitations as revoked, preventing further claim attempts.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			if len(args) > 1 {
				err := errors.New("accepts at most 1 arg(s), received multiple")
				identityWriteFailure(out, jsonFlag, err)
				return err
			}

			svc, err := inviteService(cmd, storeFileFlag, "")
			if err != nil {
				err = fmt.Errorf("failed to initialize invite store: %w", err)
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				svc.Out = nil
				svc.Err = nil
			}

			tokenOrID := ""
			if len(args) == 1 {
				tokenOrID = args[0]
			}
			res, err := svc.Revoke(tokenOrID, allFlag)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}

			if jsonFlag {
				return output.Write(out, output.Success(res))
			}

			p := render.NewPrinter(out)
			if allFlag {
				if res.None {
					p.Line(render.StatusInfo, "No active invitations to revoke.")
					return p.Err()
				}
				p.Title("Invitations Revoked")
				p.Line(render.StatusOK, fmt.Sprintf("Revoked %d active invitation(s):", len(res.Revoked)))
				for _, r := range res.Revoked {
					idCol := r.ID
					if len(idCol) > 16 {
						idCol = idCol[:16] + "…"
					}
					p.Line(render.StatusInfo, fmt.Sprintf("%s  %-26s (scope: %s)",
						render.Code.Render(padRight(idCol, 18)),
						render.Bold.Render(r.Email),
						render.Info.Render(r.Scope),
					))
				}
				return p.Err()
			}

			rec := res.Revoked[0]
			p.Title("Invitation Revoked")
			p.Line(render.StatusOK, fmt.Sprintf("Revoked invitation %s for %s (scope: %s)",
				render.Code.Render(rec.ID),
				render.Bold.Render(rec.Email),
				render.Info.Render(rec.Scope),
			))

			return p.Err()
		},
	}

	cmd.Flags().StringVar(&storeFileFlag, "store-file", "", "Custom path to invites storage file")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Revoke all active developer onboarding invitations")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
