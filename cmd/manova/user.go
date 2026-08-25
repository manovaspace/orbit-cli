package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/manovaspace/orbit-cli/pkg/user"
	"github.com/spf13/cobra"
)

func newUserCmd() *cobra.Command {
	var storePath string

	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage developer accounts, roles, and provisioned credentials",
		Long:  "Commands to list, inspect, lock, unlock, deprovision, and rotate developer accounts across LLDAP, Git, and WireGuard.",
	}

	cmd.PersistentFlags().StringVar(&storePath, "store", user.DefaultUserStoreFile, "Path to user persistence file")

	cmd.AddCommand(newUserListCmd(&storePath))
	cmd.AddCommand(newUserInspectCmd(&storePath))
	cmd.AddCommand(newUserLockCmd(&storePath))
	cmd.AddCommand(newUserUnlockCmd(&storePath))
	cmd.AddCommand(newUserDeprovisionCmd(&storePath))
	cmd.AddCommand(newUserRotateKeyCmd(&storePath))

	return cmd
}

func newUserListCmd(storePath *string) *cobra.Command {
	var (
		statusFilter string
		format       string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List provisioned developer accounts",
		Long:  "Renders a list of all developer users, roles, account statuses, WireGuard IPs, and Git usernames.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			mgr := user.NewUserManager(*storePath)

			users, err := mgr.ListUsers(cmd.Context(), statusFilter)
			if err != nil {
				return fmt.Errorf("failed to list users: %w", err)
			}

			if format == "json" {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(users)
			}

			if len(users) == 0 {
				fmt.Fprintln(out, "No users found.")
				return nil
			}

			fmt.Fprintln(out, titleStyle.Render("Manova Developer User Directory"))
			w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "UID\tNAME\tEMAIL\tROLE\tSTATUS\tVPN IP\tGIT")
			fmt.Fprintln(w, "---\t----\t-----\t----\t------\t------\t---")

			for _, u := range users {
				statusStr := string(u.Status)
				if u.Status == user.StatusActive {
					statusStr = successStyle.Render("active")
				} else if u.Status == user.StatusLocked {
					statusStr = warningStyle.Render("locked")
				}

				vpnIP := u.WireGuardIP
				if vpnIP == "" {
					vpnIP = "-"
				}
				gitUser := u.ForgejoUser
				if gitUser == "" {
					gitUser = "-"
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					codeStyle.Render(u.UID),
					u.DisplayName,
					subtleStyle.Render(u.Email),
					u.Role,
					statusStr,
					vpnIP,
					gitUser,
				)
			}
			_ = w.Flush()
			return nil
		},
	}

	cmd.Flags().StringVar(&statusFilter, "status", "all", "Filter users by status (active, locked, all)")
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table, json)")

	return cmd
}

func newUserInspectCmd(storePath *string) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "inspect <identifier>",
		Short: "Inspect a developer account and provisioned credentials",
		Long:  "Displays detailed identity, LDAP status, Git account, and WireGuard VPN credentials for a user.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			mgr := user.NewUserManager(*storePath)

			u, err := mgr.GetUser(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("user %q not found: %w", args[0], err)
			}

			if jsonOutput {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(u)
			}

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Developer Profile: %s (%s)", u.DisplayName, u.UID)))
			fmt.Fprintf(out, "  Email:       %s\n", boldStyle.Render(u.Email))
			fmt.Fprintf(out, "  Role:        %s\n", u.Role)

			statusText := string(u.Status)
			if u.Status == user.StatusActive {
				statusText = successStyle.Render("active")
			} else {
				statusText = warningStyle.Render("locked")
			}
			fmt.Fprintf(out, "  Status:      %s\n", statusText)
			if u.LockReason != "" {
				fmt.Fprintf(out, "  Lock Reason: %s\n", warningStyle.Render(u.LockReason))
			}

			fmt.Fprintln(out, "\nProvisioned Subsystems:")
			fmt.Fprintf(out, "  • LLDAP Directory:  %s (uid: %s)\n", iconOK, u.UID)
			if u.ForgejoUser != "" {
				fmt.Fprintf(out, "  • Git (Forgejo):    %s (username: %s, ssh keys: %d)\n", iconOK, u.ForgejoUser, u.SSHKeyCount)
			} else {
				fmt.Fprintf(out, "  • Git (Forgejo):    %s (not provisioned)\n", subtleStyle.Render("-"))
			}

			if u.WireGuardIP != "" {
				fmt.Fprintf(out, "  • VPN (WireGuard):  %s (ip: %s)\n", iconOK, codeStyle.Render(u.WireGuardIP))
			} else {
				fmt.Fprintf(out, "  • VPN (WireGuard):  %s (not provisioned)\n", subtleStyle.Render("-"))
			}

			fmt.Fprintf(out, "\n  Created At:  %s\n", subtleStyle.Render(u.CreatedAt.Format("2006-01-02 15:04:05 UTC")))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func newUserLockCmd(storePath *string) *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:   "lock <identifier>",
		Short: "Temporarily lock developer access across all subsystems",
		Long:  "Freezes LLDAP authentication, disables WireGuard peer, and deactivates Git access without deleting user data.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			mgr := user.NewUserManager(*storePath)

			if reason == "" {
				reason = "Administrative lock"
			}

			if err := mgr.LockUser(cmd.Context(), args[0], reason); err != nil {
				return fmt.Errorf("failed to lock user: %w", err)
			}

			fmt.Fprintf(out, "%s Locked developer account %s (Reason: %s)\n", iconOK, boldStyle.Render(args[0]), subtleStyle.Render(reason))
			return nil
		},
	}

	cmd.Flags().StringVarP(&reason, "reason", "r", "", "Reason for locking the account")

	return cmd
}

func newUserUnlockCmd(storePath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock <identifier>",
		Short: "Unlock a locked developer account",
		Long:  "Restores LLDAP authentication, re-enables WireGuard peer, and re-activates Git access.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			mgr := user.NewUserManager(*storePath)

			if err := mgr.UnlockUser(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("failed to unlock user: %w", err)
			}

			fmt.Fprintf(out, "%s Restored active access for developer account %s\n", iconOK, boldStyle.Render(args[0]))
			return nil
		},
	}

	return cmd
}

func newUserDeprovisionCmd(storePath *string) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:     "deprovision <identifier>",
		Aliases: []string{"delete", "remove"},
		Short:   "Atomically offboard and deprovision a developer account",
		Long:    "Performs zero-leak cleanup: frees WireGuard IP, deletes Git API tokens/keys, purges LDAP entry, and revokes invite tokens.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			mgr := user.NewUserManager(*storePath)

			identifier := args[0]
			if !yes {
				promptMsg := fmt.Sprintf("Are you sure you want to permanently deprovision %s and revoke all access?", identifier)
				if !promptYesNo(in, out, promptMsg, false) {
					fmt.Fprintln(out, "Deprovisioning cancelled.")
					return nil
				}
			}

			summary, err := mgr.DeprovisionUser(cmd.Context(), identifier)
			if err != nil {
				return fmt.Errorf("failed to deprovision user: %w", err)
			}

			fmt.Fprintf(out, "\n%s %s\n", iconOK, successStyle.Render(fmt.Sprintf("Developer %s offboarded successfully.", summary.Email)))
			fmt.Fprintf(out, "  • WireGuard VPN IP freed: %s\n", codeStyle.Render(summary.WireGuardFreedIP))
			fmt.Fprintf(out, "  • Git account revoked:   %s\n", iconOK)
			fmt.Fprintf(out, "  • LLDAP entry removed:   %s\n", iconOK)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation prompt")

	return cmd
}

func newUserRotateKeyCmd(storePath *string) *cobra.Command {
	var (
		secretEnv string
		jsonOut   bool
	)

	cmd := &cobra.Command{
		Use:   "rotate-key <identifier>",
		Short: "Rotate credentials and generate a fresh onboarding claim token",
		Long:  "Generates a new HMAC-SHA256 signed invitation token allowing the developer to re-claim their workspace and bind new SSH/WireGuard keys.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			mgr := user.NewUserManager(*storePath)

			secret := []byte(os.Getenv(secretEnv))
			if len(secret) == 0 {
				secret = []byte(DefaultFallbackSecret)
			}

			token, claims, err := mgr.RotateKey(cmd.Context(), args[0], secret)
			if err != nil {
				return fmt.Errorf("failed to rotate user key: %w", err)
			}

			if jsonOut {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]interface{}{
					"token":      token,
					"email":      claims.Email,
					"expires_at": claims.ExpiresAt,
				})
			}

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Key Rotation Token for %s", claims.Email)))
			fmt.Fprintf(out, "  Token:      %s\n", boldStyle.Render(token))
			fmt.Fprintf(out, "  Expires:    %s (24 hours)\n\n", subtleStyle.Render(claims.ExpiresAt.Format("2006-01-02 15:04:05 UTC")))
			fmt.Fprintf(out, "Instruct developer to run on their machine:\n")
			fmt.Fprintf(out, "  %s\n", codeStyle.Render(fmt.Sprintf("m onboard --token %s", token)))
			return nil
		},
	}

	cmd.Flags().StringVar(&secretEnv, "secret-env", DefaultSecretEnv, "Environment variable containing signing secret")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output in JSON format")

	return cmd
}
