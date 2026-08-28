package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/config"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/onboard"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/spf13/cobra"
)

const (
	DefaultSecretEnv      = "ORBIT_SIGNING_SECRET"
	DefaultFallbackSecret = "orbit-dev-insecure-invitation-signing-secret-key-32bytes"
)

var (
	version = "v0.4.2"
	commit  = "none"
	date    = "unknown"

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")).
			MarginBottom(1)

	boldStyle = lipgloss.NewStyle().
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("10"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))

	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	iconOK    = successStyle.Render("✔")
	iconArrow = subtleStyle.Render("→")
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		if (version == "dev" || version == "") && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "none" && s.Value != "" {
					if len(s.Value) > 7 {
						commit = s.Value[:7]
					} else {
						commit = s.Value
					}
				}
			case "vcs.time":
				if date == "unknown" && s.Value != "" {
					date = s.Value
				}
			}
		}
	}
}

type serverOptions struct {
	addr           string
	smtpHost       string
	smtpPort       string
	smtpUser       string
	smtpPass       string
	smtpFrom       string
	signingSecret  string
	storePath      string
	ownerStorePath string
	configPath     string
}

func formatVersion() string {
	var meta []string
	if commit != "" && commit != "none" {
		meta = append(meta, fmt.Sprintf("commit: %s", commit))
	}
	if date != "" && date != "unknown" {
		meta = append(meta, fmt.Sprintf("built: %s", date))
	}
	if len(meta) > 0 {
		return fmt.Sprintf("orbit-server version %s (%s)\n", version, strings.Join(meta, ", "))
	}
	return fmt.Sprintf("orbit-server version %s\n", version)
}

func newRootCmd() *cobra.Command {
	opts := &serverOptions{}

	cmd := &cobra.Command{
		Use:     "orbit-server",
		Short:   "Orbit infrastructure and onboarding daemon",
		Long:    "Standalone daemon providing the Orbit developer onboarding edge HTTP service and platform verification.",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(cmd, opts)
		},
	}

	cmd.SetVersionTemplate(formatVersion())
	cmd.InitDefaultVersionFlag()

	// Server listener and daemon flags
	cmd.Flags().StringVarP(&opts.addr, "addr", "a", ":8080", "HTTP server listen address (e.g. :8080 or 127.0.0.1:8080)")
	cmd.Flags().StringVar(&opts.smtpHost, "smtp-host", "", "SMTP server host (default: $ORBIT_SMTP_HOST or mail.manova.space)")
	cmd.Flags().StringVar(&opts.smtpPort, "smtp-port", "", "SMTP server port (default: $ORBIT_SMTP_PORT or 587)")
	cmd.Flags().StringVar(&opts.smtpUser, "smtp-user", "", "SMTP authentication user")
	cmd.Flags().StringVar(&opts.smtpPass, "smtp-pass", "", "SMTP authentication password")
	cmd.Flags().StringVar(&opts.smtpFrom, "smtp-from", "", "Sender email address for challenge/invite emails")
	cmd.Flags().StringVar(&opts.signingSecret, "signing-secret", "", "Cryptographic secret for token signing and verification")
	cmd.Flags().StringVar(&opts.signingSecret, "secret", "", "Alias for --signing-secret")
	_ = cmd.Flags().MarkHidden("secret")
	cmd.Flags().StringVar(&opts.storePath, "store", "", "Custom path to invites storage file")
	cmd.Flags().StringVar(&opts.ownerStorePath, "owner-store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "Custom path to configuration file")

	// Version subcommand for parity
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print orbit-server version and build metadata",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprint(cmd.OutOrStdout(), formatVersion())
		},
	}
	cmd.AddCommand(versionCmd)

	return cmd
}

func runServer(cmd *cobra.Command, opts *serverOptions) error {
	out := cmd.OutOrStdout()

	// 1. Resolve configuration from file and env
	var smtpPortInt int
	if opts.smtpPort != "" {
		if p, err := strconv.Atoi(opts.smtpPort); err == nil {
			smtpPortInt = p
		}
	}

	cfg, _ := config.Resolve(config.ResolveOptions{
		ConfigPath: opts.configPath,
		SMTPHost:   opts.smtpHost,
		SMTPPort:   smtpPortInt,
		SMTPUser:   opts.smtpUser,
		SMTPPass:   opts.smtpPass,
		SMTPFrom:   opts.smtpFrom,
	})

	// 2. Resolve SMTP parameters
	smtpHost := opts.smtpHost
	if smtpHost == "" && cfg != nil && cfg.SMTP.Host != "" {
		smtpHost = cfg.SMTP.Host
	}
	if smtpHost == "" {
		smtpHost = "mail.manova.space"
	}

	smtpPort := opts.smtpPort
	if smtpPort == "" && cfg != nil && cfg.SMTP.Port > 0 {
		smtpPort = strconv.Itoa(cfg.SMTP.Port)
	}
	if smtpPort == "" {
		smtpPort = "587"
	}

	smtpUser := opts.smtpUser
	if smtpUser == "" && cfg != nil && cfg.SMTP.User != "" {
		smtpUser = cfg.SMTP.User
	}

	smtpPass := opts.smtpPass
	if smtpPass == "" && cfg != nil && cfg.SMTP.Pass != "" {
		smtpPass = cfg.SMTP.Pass
	}

	smtpFrom := opts.smtpFrom
	if smtpFrom == "" && cfg != nil && cfg.SMTP.From != "" {
		smtpFrom = cfg.SMTP.From
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

	// 3. Resolve signing secret
	secret, secretSource := resolveSigningSecret(opts.signingSecret, opts.ownerStorePath)

	// 4. Initialize Invite Store
	var inviteStore *invite.Store
	if store, err := invite.NewStore(opts.storePath); err == nil {
		inviteStore = store
	}

	// 5. Initialize Challenge Manager
	challengeMgr := owner.NewChallengeManager()

	// 6. Initialize Onboard Server
	serverCfg := onboard.ServerConfig{
		Addr:             opts.addr,
		Secret:           []byte(secret),
		Provisioner:      provisioner.NewDevProvisioner(),
		InviteStore:      inviteStore,
		ChallengeManager: challengeMgr,
		Mailer:           mailer,
	}

	srv, err := onboard.NewServer(serverCfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// 7. Start HTTP Listener
	addr := opts.addr
	if addr == "" {
		addr = ":8080"
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer lis.Close()

	actualAddr := lis.Addr().String()

	fmt.Fprintln(out, titleStyle.Render("Orbit Infrastructure Daemon (orbit-server)"))
	fmt.Fprintf(out, "  %s  HTTP listener active on http://%s\n", iconOK, boldStyle.Render(actualAddr))
	fmt.Fprintf(out, "  %s  Mail gateway: %s\n", iconOK, subtleStyle.Render(smtpHost+":"+smtpPort))
	if secretSource != "" {
		fmt.Fprintf(out, "  %s  Signing secret: %s\n\n", iconOK, infoStyle.Render(secretSource))
	}

	// 8. Graceful Shutdown on SIGINT/SIGTERM or Context Cancellation
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errChan := make(chan error, 1)
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		} else {
			errChan <- nil
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintf(out, "\n  %s  Gracefully shutting down server...\n", iconArrow)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shut down server cleanly: %w", err)
		}
		<-errChan
		fmt.Fprintf(out, "  %s  Server shutdown complete.\n", iconOK)
		return nil
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}

func resolveSigningSecret(flagSecret, ownerStorePath string) (string, string) {
	if strings.TrimSpace(flagSecret) != "" {
		return strings.TrimSpace(flagSecret), "provided via CLI flag"
	}

	for _, envKey := range []string{
		"ORBIT_SIGNING_SECRET",
		"ORBIT_INVITE_SECRET",
		"ORBIT_JWT_SECRET",
	} {
		if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
			return val, fmt.Sprintf("loaded from $%s", envKey)
		}
	}

	ownerStore := owner.NewStore(ownerStorePath)
	if rec, err := ownerStore.LoadOwner(); err == nil && rec != nil && rec.RootSigningSecret != "" {
		return rec.RootSigningSecret, fmt.Sprintf("sealed owner vault (%s)", rec.Email)
	}

	return DefaultFallbackSecret, "development fallback secret"
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
