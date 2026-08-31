package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

const (
	defaultStaffServerURL = "https://staff.dev.manova.space"
	staffOwnerUnverified  = "platform ownership is unverified. Run 'orbit admin init --owner <email>' to verify ownership before managing staff."
)

func newStaffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "staff",
		Short: "Manage Orbit staff via the staff control plane",
		Long:  "Create, list, update, disable, enable, delete, recreate, and reset passwords for staff accounts through orbit-staff (HMAC). Reserved directory accounts are rejected: admin, authelia-bind, verdaccio-bind, verdaccio-ci.",
	}
	cmd.PersistentFlags().String("server", "", "staff server URL (or ORBIT_STAFF_URL)")
	cmd.PersistentFlags().String("owner-store", "", "path to owner.json vault")

	cmd.AddCommand(newStaffCreateCmd())
	cmd.AddCommand(newStaffListCmd())
	cmd.AddCommand(newStaffGetCmd())
	cmd.AddCommand(newStaffUpdateCmd())
	cmd.AddCommand(newStaffDisableCmd())
	cmd.AddCommand(newStaffEnableCmd())
	cmd.AddCommand(newStaffDeleteCmd())
	cmd.AddCommand(newStaffResetPasswordCmd())
	cmd.AddCommand(newStaffRecreateCmd())
	return cmd
}

func newStaffCreateCmd() *cobra.Command {
	var (
		uidFlag         string
		nameFlag        string
		forwardFlag     string
		groupsFlag      string
		totpFlag        bool
		idemFlag        string
		inviteFlag      bool
		inviteEmailFlag string
		inviteTTLFlag   string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a staff member (lldap + mailbox)",
		Long:  "Create a staff member and optionally generate a signed onboarding invite token with --invite.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			uid := strings.TrimSpace(uidFlag)
			if uid == "" {
				return errors.New("required flag --uid")
			}
			forward := strings.TrimSpace(forwardFlag)
			if forward == "" || !strings.Contains(forward, "@") {
				return errors.New("required flag --forward with an email address")
			}
			groups := splitCSV(groupsFlag)
			idem := strings.TrimSpace(idemFlag)
			if idem == "" {
				idem, err = newIdempotencyKey()
				if err != nil {
					return err
				}
			}
			res, err := c.Create(context.Background(), client.StaffCreateInput{
				UID:             uid,
				DisplayName:     strings.TrimSpace(nameFlag),
				PersonalForward: forward,
				Groups:          groups,
				TOTP:            totpFlag,
				IdempotencyKey:  idem,
			})
			if err != nil {
				return err
			}
			printStaffCreate(cmd, res)

			// --invite: generate a signed onboarding invite token and print it
			if inviteFlag {
				inviteEmail := strings.TrimSpace(inviteEmailFlag)
				if inviteEmail == "" {
					inviteEmail = forward // default to the forward address
				}
				tokenStr, invID, err := createStaffInvite(cmd, inviteEmail, strings.TrimSpace(nameFlag), inviteTTLFlag, res.OTPAuth)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "warn   invite generation failed: %v\n", err)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "invite    %s\n", invID)
					fmt.Fprintf(cmd.OutOrStdout(), "token     %s\n", tokenStr)
					fmt.Fprintf(cmd.OutOrStdout(), "web setup https://orbit.manova.space/setup?token=%s\n", tokenStr)
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&uidFlag, "uid", "", "staff uid (required)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "display name")
	cmd.Flags().StringVar(&forwardFlag, "forward", "", "personal forward email (required)")
	cmd.Flags().StringVar(&groupsFlag, "groups", "", "comma-separated groups (default server-side: dev)")
	cmd.Flags().BoolVar(&totpFlag, "totp", false, "enroll Authelia TOTP")
	cmd.Flags().StringVar(&idemFlag, "idempotency-key", "", "idempotency key (generated if empty)")
	cmd.Flags().BoolVar(&inviteFlag, "invite", false, "generate and print a signed onboarding invite token after account creation")
	cmd.Flags().StringVar(&inviteEmailFlag, "invite-email", "", "email address for the invite token (defaults to --forward value)")
	cmd.Flags().StringVar(&inviteTTLFlag, "invite-ttl", "7d", "invite token TTL (e.g. 7d, 24h, 168h)")
	_ = cmd.MarkFlagRequired("uid")
	_ = cmd.MarkFlagRequired("forward")
	return cmd
}

func newStaffListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List staff members",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			list, err := c.List(context.Background())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			tbl := table.New(
				table.Column{Title: "UID", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 12},
				table.Column{Title: "NAME", HeaderStyle: headerStyle, MinWidth: 16, Flexible: true},
				table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 10},
				table.Column{Title: "FORWARD EMAIL", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 20, Flexible: true},
			)

			for _, s := range list {
				var statusCell table.Cell
				switch strings.ToLower(s.Status) {
				case "active", "enabled":
					statusCell = table.StyledCell("✔ active", successStyle.Render("✔ active"))
				case "disabled":
					statusCell = table.StyledCell("✖ disabled", errorStyle.Render("✖ disabled"))
				default:
					statusCell = table.PlainCell(s.Status)
				}

				nameVal := s.DisplayName
				if nameVal == "" {
					nameVal = "-"
				}

				tbl.AddStyledRow(
					table.PlainCell(s.UID),
					table.PlainCell(nameVal),
					statusCell,
					table.PlainCell(s.PersonalForward),
				)
			}

			_ = tbl.Render(out)
			return nil
		},
	}
}

func newStaffGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Get a staff member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			s, err := c.Get(context.Background(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "uid    %s\n", s.UID)
			fmt.Fprintf(out, "name   %s\n", s.DisplayName)
			fmt.Fprintf(out, "mail   %s\n", s.Mail)
			fmt.Fprintf(out, "fwd    %s\n", s.PersonalForward)
			fmt.Fprintf(out, "status %s\n", s.Status)
			if len(s.Groups) > 0 {
				fmt.Fprintf(out, "groups %s\n", strings.Join(s.Groups, ","))
			}
			return nil
		},
	}
}

func newStaffUpdateCmd() *cobra.Command {
	var (
		nameFlag    string
		forwardFlag string
		groupsFlag  string
	)
	cmd := &cobra.Command{
		Use:   "update <uid>",
		Short: "Update display name, forward, or groups",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			s, err := c.Update(context.Background(), args[0], client.StaffUpdateInput{
				DisplayName:     strings.TrimSpace(nameFlag),
				PersonalForward: strings.TrimSpace(forwardFlag),
				Groups:          splitCSV(groupsFlag),
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "updated %s\n", s.UID)
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "display name")
	cmd.Flags().StringVar(&forwardFlag, "forward", "", "personal forward email")
	cmd.Flags().StringVar(&groupsFlag, "groups", "", "comma-separated groups")
	return cmd
}

func newStaffDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <uid>",
		Short: "Disable a staff member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			if err := c.Disable(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "disabled %s\n", args[0])
			return nil
		},
	}
}

func newStaffEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <uid>",
		Short: "Enable a staff member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			if err := c.Enable(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "enabled %s\n", args[0])
			return nil
		},
	}
}

func newStaffDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete a staff member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			if err := c.Delete(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
			return nil
		},
	}
}

func newStaffResetPasswordCmd() *cobra.Command {
	var ldapFlag, mailboxFlag, totpFlag bool
	cmd := &cobra.Command{
		Use:   "reset-password <uid>",
		Short: "Rotate ldap and/or mailbox passwords, optionally Authelia TOTP",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			res, err := c.ResetPassword(context.Background(), args[0], ldapFlag, mailboxFlag, totpFlag)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "uid    %s\n", args[0])
			if res.LDAPPassword != "" {
				fmt.Fprintf(out, "sso    %s\n", res.LDAPPassword)
			}
			if res.MailPassword != "" {
				fmt.Fprintf(out, "mail   %s\n", res.MailPassword)
			}
			if res.OTPAuth != "" {
				fmt.Fprintf(out, "otpauth %s\n", res.OTPAuth)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&ldapFlag, "ldap", false, "reset SSO/ldap password")
	cmd.Flags().BoolVar(&mailboxFlag, "mailbox", false, "reset mailbox password")
	cmd.Flags().BoolVar(&totpFlag, "totp", false, "replace Authelia TOTP and print otpauth")
	return cmd
}

func newStaffRecreateCmd() *cobra.Command {
	var (
		uidFlag     string
		nameFlag    string
		forwardFlag string
		groupsFlag  string
		totpFlag    bool
		idemFlag    string
	)
	cmd := &cobra.Command{
		Use:   "recreate",
		Short: "Delete then create a staff member (fresh SSO + mailbox)",
		Long:  "Wipes Authelia TOTP, the Stalwart mailbox, and the lldap user, then creates them again. Reserved directory accounts (admin, authelia-bind, verdaccio-bind, verdaccio-ci) are rejected.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := staffClientFromCmd(cmd)
			if err != nil {
				return err
			}
			uid := strings.TrimSpace(uidFlag)
			if uid == "" {
				return errors.New("required flag --uid")
			}
			forward := strings.TrimSpace(forwardFlag)
			if forward == "" || !strings.Contains(forward, "@") {
				return errors.New("required flag --forward with an email address")
			}
			if err := c.Delete(context.Background(), uid); err != nil && !staffIsNotFound(err) {
				return err
			}
			idem := strings.TrimSpace(idemFlag)
			if idem == "" {
				idem, err = newIdempotencyKey()
				if err != nil {
					return err
				}
			}
			res, err := c.Create(context.Background(), client.StaffCreateInput{
				UID:             uid,
				DisplayName:     strings.TrimSpace(nameFlag),
				PersonalForward: forward,
				Groups:          splitCSV(groupsFlag),
				TOTP:            totpFlag,
				IdempotencyKey:  idem,
			})
			if err != nil {
				return err
			}
			printStaffCreate(cmd, res)
			return nil
		},
	}
	cmd.Flags().StringVar(&uidFlag, "uid", "", "staff uid (required)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "display name")
	cmd.Flags().StringVar(&forwardFlag, "forward", "", "personal forward email (required)")
	cmd.Flags().StringVar(&groupsFlag, "groups", "", "comma-separated groups (default server-side: dev)")
	cmd.Flags().BoolVar(&totpFlag, "totp", false, "enroll Authelia TOTP after recreate")
	cmd.Flags().StringVar(&idemFlag, "idempotency-key", "", "idempotency key for create (generated if empty)")
	_ = cmd.MarkFlagRequired("uid")
	_ = cmd.MarkFlagRequired("forward")
	return cmd
}

func staffIsNotFound(err error) bool {
	var api *client.APIError
	return errors.As(err, &api) && api.StatusCode == http.StatusNotFound
}

func staffClientFromCmd(cmd *cobra.Command) (*client.StaffClient, error) {
	ownerStoreFlag := staffPersistentString(cmd, "owner-store")
	store := owner.NewStore(ownerStoreFlag)
	rec, err := store.LoadOwner()
	if err != nil || rec == nil || !rec.IsVerified() {
		return nil, errors.New(staffOwnerUnverified)
	}

	server := strings.TrimSpace(staffPersistentString(cmd, "server"))
	if server == "" {
		server = strings.TrimSpace(os.Getenv("ORBIT_STAFF_URL"))
	}
	if server == "" {
		server = defaultStaffServerURL
	}

	return client.NewStaffClient(server, rec.RootSigningSecret), nil
}

func staffPersistentString(cmd *cobra.Command, name string) string {
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.PersistentFlags().Lookup(name); f != nil {
			return f.Value.String()
		}
	}
	if v, err := cmd.Flags().GetString(name); err == nil {
		return v
	}
	return ""
}

func printStaffCreate(cmd *cobra.Command, res *client.StaffCreateResult) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "uid    %s\n", res.UID)
	if res.LDAPPassword != "" {
		fmt.Fprintf(out, "sso    %s\n", res.LDAPPassword)
	}
	if res.MailPassword != "" {
		fmt.Fprintf(out, "mail   %s\n", res.MailPassword)
	}
	fmt.Fprintf(out, "fwd    %s\n", res.PersonalForward)
	if res.OTPAuth != "" {
		fmt.Fprintf(out, "otpauth %s\n", res.OTPAuth)
	}
	if res.Idempotent {
		fmt.Fprintf(out, "idempotent true\n")
	}
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func newIdempotencyKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// createStaffInvite generates a signed onboarding invite token for a newly created staff member.
// It resolves the signing secret from the owner store or environment, generates the token,
// saves it to the invite store, and returns the token string and invite ID.
func createStaffInvite(cmd *cobra.Command, email, displayName, ttlStr, otpauth string) (tokenStr, inviteID string, err error) {
	ownerStoreFlag := staffPersistentString(cmd, "owner-store")
	ownerStore := owner.NewStore(ownerStoreFlag)
	ownerRecord, _ := ownerStore.LoadOwner()

	var secret []byte
	if ownerRecord != nil && ownerRecord.IsVerified() && ownerRecord.RootSigningSecret != "" {
		secret = []byte(ownerRecord.RootSigningSecret)
	} else if env := os.Getenv("ORBIT_INVITE_SECRET"); env != "" {
		secret = []byte(env)
	} else if env := os.Getenv("MANOVA_INVITE_SECRET"); env != "" {
		secret = []byte(env)
	} else {
		secret = []byte(DefaultFallbackSecret)
	}

	ttl, err := parseDuration(ttlStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid --invite-ttl: %w", err)
	}

	createdBy := ""
	if ownerRecord != nil && ownerRecord.IsVerified() {
		createdBy = ownerRecord.Email
	}

	var metadata map[string]string
	if strings.TrimSpace(otpauth) != "" {
		metadata = map[string]string{
			"otpauth": strings.TrimSpace(otpauth),
		}
	}

	req := invite.InviteRequest{
		Email:       email,
		DisplayName: displayName,
		Scope:       "core",
		TTL:         ttl,
		CreatedBy:   createdBy,
		Metadata:    metadata,
	}

	tokenStr, claims, err := invite.GenerateToken(req, secret)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate invite token: %w", err)
	}

	store, err := invite.NewStore("")
	if err != nil {
		return "", "", fmt.Errorf("failed to open invite store: %w", err)
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
		return "", "", fmt.Errorf("failed to save invite: %w", err)
	}

	return tokenStr, claims.ID, nil
}
