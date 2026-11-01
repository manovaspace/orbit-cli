package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/config/application"
	configdomain "github.com/manovaspace/orbit-cli/pkg/config/domain"
	"github.com/manovaspace/orbit-cli/pkg/output"
	ownerapp "github.com/manovaspace/orbit-cli/pkg/owner/application"
	ownerfilestore "github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/filestore"
	ownerhttpapi "github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/httpapi"
	ownersmtp "github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/smtp"
	ownertelegram "github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/telegram"
	"github.com/spf13/cobra"
)

// adminWiring gathers what each admin command needs to build its service.
type adminWiring struct {
	storePath string
	serverURL string
	out       io.Writer
	errOut    io.Writer
	prompts   ownerapp.Prompts
	telegram  ownerapp.Telegram
}

func newAdminService(w adminWiring) *ownerapp.Service {
	svc := &ownerapp.Service{
		Vault:     ownerfilestore.NewVault(w.storePath),
		Out:       w.out,
		Err:       w.errOut,
		Now:       func() time.Time { return time.Now().UTC() },
		LookupEnv: lookupEnv,
		UI:        adminUI(),
		Prompts:   w.prompts,
		MailerFactory: func(cfg ownerapp.SMTPConfig) ownerapp.Mailer {
			return ownersmtp.New(cfg)
		},
	}
	if w.serverURL != "" {
		svc.Server = ownerhttpapi.New(w.serverURL)
	}
	if w.telegram != nil {
		svc.Telegram = w.telegram
	}
	return svc
}

func adminUI() ownerapp.UI {
	return ownerapp.UI{
		Arrow: iconArrow,
		OK:    iconOK,
		Warn:  iconWarn,
		Info:  iconInfo,
		Error: iconError,
		Title: func(text string) string { return titleStyle.Render(text) },
		Header: func(text string) string {
			return headerStyle.Render(text)
		},
		Bold:    func(text string) string { return boldStyle.Render(text) },
		Subtle:  func(text string) string { return subtleStyle.Render(text) },
		Code:    func(text string) string { return codeStyle.Render(text) },
		Success: func(text string) string { return successStyle.Render(text) },
		Warning: func(text string) string { return warningStyle.Render(text) },
		ErrorStyle: func(text string) string {
			return errorStyle.Render(text)
		},
		Card: renderCard,
	}
}

// identityWriteFailure emits the JSON error envelope when --json or --format json is set.
func identityWriteFailure(out io.Writer, jsonFlag bool, err error) {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
}

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
		jsonFlag   bool
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

			cfg, err := newConfigService(out).Resolve(application.ResolveOptions{
				ConfigPath: getConfigPath(cmd),
				ServerFlag: serverFlag,
			})
			if err != nil {
				err = clierr.Wrap(err, clierr.Failed("failed to resolve configuration"))
				identityWriteFailure(out, jsonFlag, err)
				return err
			}

			email := strings.TrimSpace(ownerFlag)
			if len(args) > 0 && email == "" {
				email = strings.TrimSpace(args[0])
			}

			serverURL := strings.TrimSpace(cfg.Config.Server.URL)
			if serverURL == "" {
				serverURL = configdomain.DefaultServerURL
			}

			svc := newAdminService(adminWiring{
				storePath: storeFlag,
				serverURL: serverURL,
				out:       uiWriter(out, jsonFlag),
				errOut:    uiWriter(cmd.ErrOrStderr(), jsonFlag),
				prompts: ownerapp.Prompts{
					Email: func() string {
						return promptString(in, uiWriter(out, jsonFlag), "Enter platform owner email address", "")
					},
					Code: func() string {
						return promptString(in, uiWriter(out, jsonFlag), "Enter 6-digit OTP or 8-digit grant code", "")
					},
				},
			})

			res, err := svc.Init(cmd.Context(), ownerapp.InitInput{
				Email:       email,
				DisplayName: nameFlag,
				Code:        codeFlag,
				ServerURL:   serverURL,
				Force:       forceFlag,
				NoSend:      noSendFlag,
			})
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
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
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

func newAdminStatusCmd() *cobra.Command {
	var (
		storeFlag  string
		formatFlag string
		jsonFlag   bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display platform ownership verification status, vault integrity, and mail config",
		Long:  "Reports whether platform ownership has been verified, vault file path and permissions, and active SMTP gateway.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			asJSON := jsonFlag || strings.ToLower(formatFlag) == "json"

			svc := newAdminService(adminWiring{
				storePath: storeFlag,
				out:       uiWriter(out, asJSON),
				errOut:    uiWriter(cmd.ErrOrStderr(), asJSON),
			})

			res := svc.Status()
			if asJSON {
				return output.Write(out, output.Success(res))
			}
			svc.WriteStatus(res)
			return nil
		},
	}

	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format: table or json")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

func newAdminVerifyCmd() *cobra.Command {
	var (
		ownerFlag string
		codeFlag  string
		nameFlag  string
		storeFlag string
		forceFlag bool
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "verify [email] [code]",
		Short: "Seal owner.json locally (hermetic OTP; no API call)",
		Long:  "Local-only verification. The code is checked in-process, not against orbit-server or the gateway. On success a new master secret is sealed in owner.json. Does not import a server fingerprint.",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

			email := strings.TrimSpace(ownerFlag)
			if len(args) > 0 && email == "" {
				email = strings.TrimSpace(args[0])
			}
			code := strings.TrimSpace(codeFlag)
			if len(args) > 1 && code == "" {
				code = strings.TrimSpace(args[1])
			}

			svc := newAdminService(adminWiring{
				storePath: storeFlag,
				out:       uiWriter(out, jsonFlag),
				errOut:    uiWriter(cmd.ErrOrStderr(), jsonFlag),
				prompts: ownerapp.Prompts{
					Email: func() string {
						return promptString(in, uiWriter(out, jsonFlag), "Enter owner email address", "")
					},
					Code: func() string {
						return promptString(in, uiWriter(out, jsonFlag), "Enter 6-digit verification code", "")
					},
				},
			})

			res, err := svc.Verify(cmd.Context(), ownerapp.VerifyInput{
				Email:       email,
				Code:        code,
				DisplayName: nameFlag,
				Force:       forceFlag,
			})
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&ownerFlag, "owner", "o", "", "Owner email address")
	cmd.Flags().StringVarP(&codeFlag, "code", "c", "", "6-digit verification code")
	cmd.Flags().StringVarP(&nameFlag, "name", "n", "", "Owner display name")
	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force re-verification even if already verified")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

func newAdminRotateSecretCmd() *cobra.Command {
	var (
		storeFlag string
		yesFlag   bool
		jsonFlag  bool
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

			svc := newAdminService(adminWiring{
				storePath: storeFlag,
				out:       uiWriter(out, jsonFlag),
				errOut:    uiWriter(cmd.ErrOrStderr(), jsonFlag),
				prompts: ownerapp.Prompts{
					ConfirmRotate: func(email string) bool {
						msg := "Rotating the master signing secret will invalidate all existing invitation tokens for " + email + ". Proceed?"
						return promptYesNo(in, uiWriter(out, jsonFlag), msg, false)
					},
				},
			})

			res, err := svc.RotateSecret(cmd.Context(), ownerapp.RotateInput{Yes: yesFlag})
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Skip interactive confirmation prompt")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

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

			cfg, err := newConfigService(out).Resolve(application.ResolveOptions{
				ConfigPath: getConfigPath(cmd),
				ServerFlag: serverFlag,
			})
			if err != nil {
				err = clierr.Wrap(err, clierr.Failed("failed to resolve config"))
				identityWriteFailure(out, jsonFlag, err)
				return err
			}

			serverURL := strings.TrimSpace(cfg.Config.Server.URL)
			if serverURL == "" {
				serverURL = configdomain.DefaultServerURL
			}

			svc := newAdminService(adminWiring{
				storePath: storeFlag,
				serverURL: serverURL,
				out:       uiWriter(out, jsonFlag),
				errOut:    uiWriter(cmd.ErrOrStderr(), jsonFlag),
			})

			input := ownerapp.GrantInput{
				Email:     args[0],
				Role:      roleFlag,
				Code:      codeFlag,
				TTL:       ttlFlag,
				ServerURL: serverURL,
				Send:      sendFlag,
				Telegram:  telegramFlag,
				Quiet:     jsonFlag,
			}
			if telegramFlag {
				tgCfg, tgErr := ownertelegram.ResolveTelegramConfig()
				if tgErr != nil {
					if !jsonFlag {
						fmt.Fprintf(cmd.ErrOrStderr(), "  %s  Telegram dispatch skipped: %v\n", iconWarn, tgErr)
					}
				} else {
					svc.Telegram = &ownertelegram.Dispatcher{Config: tgCfg}
				}
			}

			res, err := svc.Grant(cmd.Context(), input)
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
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
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

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

			svc := newAdminService(adminWiring{
				storePath: storeFlag,
				serverURL: strings.TrimSpace(serverFlag),
				out:       uiWriter(out, jsonFlag),
				errOut:    uiWriter(cmd.ErrOrStderr(), jsonFlag),
			})

			res, err := svc.ResetTOTP(cmd.Context(), ownerapp.ResetTOTPInput{Email: args[0]})
			if err != nil {
				identityWriteFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&storeFlag, "store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Orbit server URL")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

// uiWriter discards human output when the command prints a JSON envelope.
func uiWriter(w io.Writer, quiet bool) io.Writer {
	if quiet {
		return io.Discard
	}
	return w
}

// lookupEnv reads the process environment; a variable keeps tests hermetic.
func lookupEnv(key string) string {
	return os.Getenv(key)
}
