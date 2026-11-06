package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	inviteapp "github.com/manovaspace/orbit-cli/pkg/invite/application"
	invitefilestore "github.com/manovaspace/orbit-cli/pkg/invite/infrastructure/filestore"
	inviteownerstore "github.com/manovaspace/orbit-cli/pkg/invite/infrastructure/ownerstore"
	invitesmtp "github.com/manovaspace/orbit-cli/pkg/invite/infrastructure/smtp"
	"github.com/manovaspace/orbit-cli/pkg/output"
	ownerfilestore "github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/filestore"
	"github.com/manovaspace/orbit-cli/pkg/render"
	staffapp "github.com/manovaspace/orbit-cli/pkg/staff/application"
	"github.com/manovaspace/orbit-cli/pkg/staff/domain"
	"github.com/manovaspace/orbit-cli/pkg/staff/infrastructure/inviteissuer"
	"github.com/manovaspace/orbit-cli/pkg/staff/infrastructure/ownerstore"
	"github.com/manovaspace/orbit-cli/pkg/staff/infrastructure/staffapi"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

const (
	defaultStaffServerURL     = "https://staff.internal.manova.space"
	defaultKeycloakAccountURL = "https://identity.internal.manova.space/realms/staff/account"
)

func newStaffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "staff",
		Short: "Manage Orbit staff via the staff control plane",
		Long:  "Create, list, update, disable, enable, delete, and reset passwords for staff accounts through orbit-staff (HMAC). Recreate is disabled because Keycloak retains federated profiles. Reserved directory accounts are rejected: admin, keycloak-bind, stalwart-bind, verdaccio-bind, verdaccio-ci.",
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

// staffService builds the staff use-case layer with the real HMAC client.
func staffService(cmd *cobra.Command) (*staffapp.Service, error) {
	ownerStore := ownerfilestore.NewStore(staffPersistentString(cmd, "owner-store"))
	vault := ownerstore.New(ownerStore)
	_, secret, verified := vault.Owner()
	if !verified {
		return nil, clierr.Auth(staffapp.UnverifiedOwner)
	}

	server := strings.TrimSpace(staffPersistentString(cmd, "server"))
	if server == "" {
		server = strings.TrimSpace(os.Getenv("ORBIT_STAFF_URL"))
	}
	if server == "" {
		server = defaultStaffServerURL
	}

	store, err := invitefilestore.NewStore("")
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to open invite store"))
	}
	inviteSvc := &inviteapp.Service{
		Store:     store,
		Owner:     inviteownerstore.New(ownerStore),
		LookupEnv: lookupEnv,
	}

	return &staffapp.Service{
		Directory: staffapi.New(client.NewStaffClient(server, secret)),
		Vault:     vault,
		Inviter:   &inviteissuer.Issuer{Service: inviteSvc},
		Mailer:    &inviteissuer.Mailer{Sender: invitesmtp.NewMailerFromEnv()},
		LookupEnv: lookupEnv,
	}, nil
}

func newStaffCreateCmd() *cobra.Command {
	var (
		uidFlag         string
		nameFlag        string
		firstNameFlag   string
		lastNameFlag    string
		forwardFlag     string
		groupsFlag      string
		idemFlag        string
		inviteFlag      bool
		inviteEmailFlag string
		inviteTTLFlag   string
		noSendFlag      bool
		jsonFlag        bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a staff member (LLDAP + retained mailbox)",
		Long:  "Create a staff member and optionally generate a signed onboarding invite token with --invite.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}

			res, err := svc.Create(cmd.Context(), staffapp.CreateInput{
				UID:            uidFlag,
				DisplayName:    nameFlag,
				FirstName:      firstNameFlag,
				LastName:       lastNameFlag,
				Forward:        forwardFlag,
				Groups:         groupsFlag,
				IdempotencyKey: idemFlag,
				Invite:         inviteFlag,
				InviteEmail:    inviteEmailFlag,
				InviteTTL:      inviteTTLFlag,
				NoSend:         noSendFlag,
			})
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}

			if jsonFlag {
				return output.Write(out, output.Success(res))
			}

			p := render.NewPrinter(out)
			printStaffCreate(out, res.Member)
			if res.Invite != nil {
				if res.InviteWarning != "" {
					p.Line(render.StatusWarn, res.InviteWarning)
					return p.Err()
				}
				p.Line(render.StatusInfo, "invite    "+res.Invite.ID)
				p.Line(render.StatusInfo, "token     "+res.Invite.Token)
				p.Line(render.StatusOK, "web setup https://orbit.manova.space/setup?token="+res.Invite.Token)
				if res.Invite.DispatchErr != "" {
					render.NewPrinter(cmd.ErrOrStderr()).Line(render.StatusWarn, fmt.Sprintf("failed to dispatch invitation email to %s: %s", res.Invite.Email, res.Invite.DispatchErr))
				} else if res.Invite.Dispatched {
					p.Line(render.StatusOK, "mail      dispatched to "+res.Invite.Email)
				}
			}
			return p.Err()
		},
	}
	cmd.Flags().StringVar(&uidFlag, "uid", "", "staff uid (required)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "display name")
	cmd.Flags().StringVar(&firstNameFlag, "first-name", "", "first name")
	cmd.Flags().StringVar(&lastNameFlag, "last-name", "", "last name")
	cmd.Flags().StringVar(&forwardFlag, "forward", "", "personal forward email (required)")
	cmd.Flags().StringVar(&groupsFlag, "groups", "", "comma-separated groups (default server-side: dev)")
	cmd.Flags().StringVar(&idemFlag, "idempotency-key", "", "idempotency key (generated if empty)")
	cmd.Flags().BoolVar(&inviteFlag, "invite", false, "generate and print a signed onboarding invite token after account creation")
	cmd.Flags().StringVar(&inviteEmailFlag, "invite-email", "", "email address for the invite token (defaults to --forward value)")
	cmd.Flags().StringVar(&inviteTTLFlag, "invite-ttl", "7d", "invite token TTL (e.g. 7d, 24h, 168h)")
	cmd.Flags().BoolVar(&noSendFlag, "no-send", false, "suppress dispatching onboarding invitation email")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	_ = cmd.MarkFlagRequired("uid")
	_ = cmd.MarkFlagRequired("forward")
	return cmd
}

func newStaffListCmd() *cobra.Command {
	var (
		pageFlag  int
		limitFlag int
		jsonFlag  bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List staff members",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			members, err := svc.List(cmd.Context())
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}

			if jsonFlag {
				if members == nil {
					members = []domain.Member{}
				}
				return output.Write(out, output.Success(members))
			}

			tbl := table.New(
				table.Column{Title: "UID", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 12},
				table.Column{Title: "NAME", HeaderStyle: headerStyle, MinWidth: 16, Flexible: true},
				table.Column{Title: "STATUS", HeaderStyle: headerStyle, MinWidth: 10},
				table.Column{Title: "FORWARD EMAIL", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 20, Flexible: true},
			)
			if limitFlag > 0 {
				tbl.WithPagination(pageFlag, limitFlag)
			}

			for _, s := range members {
				var statusCell table.Cell
				switch strings.ToLower(s.Status) {
				case "active", "enabled":
					statusCell = table.StyleCell("✔ active", successStyle)
				case "disabled":
					statusCell = table.StyleCell("✖ disabled", errorStyle)
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

			return render.NewPrinter(out).Table(tbl)
		},
	}
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum rows per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newStaffGetCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "get <uid>",
		Short: "Get a staff member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			member, err := svc.Get(cmd.Context(), args[0])
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(member))
			}

			p := render.NewPrinter(out)
			p.KVs(
				[2]string{"uid", member.UID},
				[2]string{"name", member.DisplayName},
				[2]string{"first", member.FirstName},
				[2]string{"last", member.LastName},
				[2]string{"mail", member.Mail},
				[2]string{"fwd", member.PersonalForward},
				[2]string{"status", member.Status},
			)
			if len(member.Groups) > 0 {
				p.KV("groups", strings.Join(member.Groups, ","))
			}
			return p.Err()
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newStaffUpdateCmd() *cobra.Command {
	var (
		nameFlag      string
		firstNameFlag string
		lastNameFlag  string
		forwardFlag   string
		groupsFlag    string
		jsonFlag      bool
	)
	cmd := &cobra.Command{
		Use:   "update <uid>",
		Short: "Update display name, profile names, forward, or groups",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			member, err := svc.Update(cmd.Context(), args[0], domain.UpdateInput{
				DisplayName:     strings.TrimSpace(nameFlag),
				FirstName:       strings.TrimSpace(firstNameFlag),
				LastName:        strings.TrimSpace(lastNameFlag),
				PersonalForward: strings.TrimSpace(forwardFlag),
				Groups:          domain.SplitGroups(groupsFlag),
			})
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(member))
			}
			p := render.NewPrinter(out)
			p.Line(render.StatusOK, "updated "+member.UID)
			return p.Err()
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "display name")
	cmd.Flags().StringVar(&firstNameFlag, "first-name", "", "first name")
	cmd.Flags().StringVar(&lastNameFlag, "last-name", "", "last name")
	cmd.Flags().StringVar(&forwardFlag, "forward", "", "personal forward email")
	cmd.Flags().StringVar(&groupsFlag, "groups", "", "comma-separated groups")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newStaffDisableCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "disable <uid>",
		Short: "Disable a staff member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if err := svc.Disable(cmd.Context(), args[0]); err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(staffAction{UID: args[0], Status: "disabled"}))
			}
			p := render.NewPrinter(out)
			p.Line(render.StatusOK, "disabled "+args[0])
			return p.Err()
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newStaffEnableCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "enable <uid>",
		Short: "Enable a staff member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if err := svc.Enable(cmd.Context(), args[0]); err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(staffAction{UID: args[0], Status: "enabled"}))
			}
			p := render.NewPrinter(out)
			p.Line(render.StatusOK, "enabled "+args[0])
			return p.Err()
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newStaffDeleteCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete a staff member",
		Long:  "Delete the staff member's LLDAP identity. The Stalwart mailbox, messages, and forwarding remain, but Keycloak browser sign-in and LLDAP-backed mail authentication stop. This command does not remove the imported Keycloak profile or validate migration backups; use the reviewed identity-reset procedure for a full reset.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if err := svc.Delete(cmd.Context(), args[0]); err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(staffAction{UID: args[0], Status: "deleted"}))
			}
			p := render.NewPrinter(out)
			p.Line(render.StatusOK, "deleted "+args[0])
			p.Line(render.StatusWarn, "Keycloak profile remains; mailbox data is preserved but LLDAP-backed mail sign-in is unavailable until the UID is restored")
			return p.Err()
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newStaffResetPasswordCmd() *cobra.Command {
	var ldapFlag, mailboxFlag, jsonFlag bool
	cmd := &cobra.Command{
		Use:   "reset-password <uid>",
		Short: "Rotate the LLDAP password used for staff SSO and mailbox sign-in",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			svc, err := staffService(cmd)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			res, err := svc.ResetPassword(cmd.Context(), args[0], ldapFlag, mailboxFlag)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			p := render.NewPrinter(out)
			p.Line(render.StatusInfo, "uid    "+args[0])
			if res.LDAPPassword != "" {
				p.Line(render.StatusOK, "shared "+res.LDAPPassword)
			}
			if res.Warning != "" {
				p.Line(render.StatusWarn, res.Warning)
			}
			return p.Err()
		},
	}
	cmd.Flags().BoolVar(&ldapFlag, "ldap", false, "reset the shared LLDAP password used for SSO and mail")
	cmd.Flags().BoolVar(&mailboxFlag, "mailbox", false, "deprecated alias for the shared LLDAP password")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newStaffRecreateCmd() *cobra.Command {
	var (
		uidFlag         string
		nameFlag        string
		forwardFlag     string
		groupsFlag      string
		idemFlag        string
		inviteFlag      bool
		inviteEmailFlag string
		inviteTTLFlag   string
		noSendFlag      bool
		jsonFlag        bool
	)
	cmd := &cobra.Command{
		Use:   "recreate",
		Short: "Deprecated: use the safe Keycloak identity reset procedure",
		Long:  "This command no longer deletes or recreates staff identities. Keycloak retains federated user profiles and Google/passkey credentials after an LLDAP deletion. Before deleting the LLDAP identity, deploy remove-keycloak-staff-user.py on the identity host and require its --uid <uid> --preflight check to succeed. Stop if it is unavailable or preflight fails. Then follow the staff lifecycle guide: delete the LLDAP identity, review a cleanup dry run, execute with --confirm <uid>, and create only after cleanup succeeds. Mailbox contents remain preserved.",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := &staffapp.Service{}
			err := svc.Recreate(uidFlag)
			if err != nil {
				identityWriteFailure(cmd.OutOrStdout(), jsonFlag, err)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&uidFlag, "uid", "", "UID to include in the safe reset instructions (legacy flag)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "legacy flag; ignored because recreate is disabled")
	cmd.Flags().StringVar(&forwardFlag, "forward", "", "legacy flag; ignored because recreate is disabled")
	cmd.Flags().StringVar(&groupsFlag, "groups", "", "legacy flag; ignored because recreate is disabled")
	cmd.Flags().StringVar(&idemFlag, "idempotency-key", "", "legacy flag; ignored because recreate is disabled")
	cmd.Flags().BoolVar(&inviteFlag, "invite", false, "legacy flag; ignored because recreate is disabled")
	cmd.Flags().StringVar(&inviteEmailFlag, "invite-email", "", "legacy flag; ignored because recreate is disabled")
	cmd.Flags().StringVar(&inviteTTLFlag, "invite-ttl", "7d", "legacy flag; ignored because recreate is disabled")
	cmd.Flags().BoolVar(&noSendFlag, "no-send", false, "legacy flag; ignored because recreate is disabled")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

// staffAction is the machine-readable result of the simple staff operations.
type staffAction struct {
	UID    string `json:"uid"`
	Status string `json:"status"`
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

func printStaffCreate(out io.Writer, res *domain.CreateResult) {
	if res == nil {
		return
	}
	p := render.NewPrinter(out)
	identity := [][2]string{{"uid", res.UID}}
	if res.FirstName != "" || res.LastName != "" {
		identity = append(identity, [2]string{"first", res.FirstName}, [2]string{"last", res.LastName})
	}
	p.KVs(identity...)
	if res.LDAPPassword != "" || res.PersonalForward != "" {
		p.KVs([2]string{"shared", res.LDAPPassword}, [2]string{"fwd", res.PersonalForward})
	}
	if res.Idempotent {
		p.Line(render.StatusInfo, "idempotent true")
	} else {
		p.Line(render.StatusInfo, "next   sign in at "+defaultKeycloakAccountURL+" with this LLDAP password, link Google, and register a passkey")
	}
}
