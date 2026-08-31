package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/config"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/onboard"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
	"github.com/spf13/cobra"
)

const (
	DefaultSecretEnv      = "ORBIT_SIGNING_SECRET"
	DefaultFallbackSecret = "orbit-dev-insecure-invitation-signing-secret-key-32bytes"
)

var (
	version = "v0.7.0"
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
	addr                    string
	smtpHost                string
	smtpPort                string
	smtpUser                string
	smtpPass                string
	smtpFrom                string
	signingSecret           string
	storePath               string
	ownerStorePath          string
	configPath              string
	disablePublicChallenges bool
	dbPath                  string
	trustedProxies          []string
}

// DefaultDBPath returns the resolved default path to the SQLite database file.
// Resolution order:
// 1. $ORBIT_DB_PATH environment variable
// 2. ~/.config/orbit/orbit.db (user home directory)
// 3. /var/lib/orbit/orbit.db or /etc/orbit/orbit.db (system fallback)
func DefaultDBPath() string {
	if envPath := os.Getenv("ORBIT_DB_PATH"); envPath != "" {
		return envPath
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "orbit", "orbit.db")
	}
	if _, err := os.Stat("/var/lib/orbit"); err == nil {
		return "/var/lib/orbit/orbit.db"
	}
	return "/etc/orbit/orbit.db"
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
		Use:   "orbit-server",
		Short: "Dedicated edge HTTP daemon for developer onboarding and infrastructure management",
		Long: `orbit-server is the dedicated edge service that validates HMAC-signed developer invitations,
orchestrates local development infrastructure, handles admin ownership verification challenges,
and serves the canonical developer onboarding script.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
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
	cmd.Flags().StringVar(&opts.storePath, "store", "", "Custom path to legacy invites storage file")
	cmd.Flags().StringVar(&opts.ownerStorePath, "owner-store", "", "Custom path to owner storage vault file")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "Custom path to configuration file")
	cmd.Flags().BoolVar(&opts.disablePublicChallenges, "disable-public-challenges", false, "Disable unauthenticated public challenge emails; require owner-issued 8-digit admin grants")
	cmd.Flags().StringVar(&opts.dbPath, "db", "", "Custom path to SQLite database (default: $ORBIT_DB_PATH or ~/.config/orbit/orbit.db)")
	cmd.Flags().StringVar(&opts.dbPath, "db-path", "", "Alias for --db")
	_ = cmd.Flags().MarkHidden("db-path")
	cmd.Flags().StringSliceVar(&opts.trustedProxies, "trusted-proxies", nil, "List of trusted reverse proxy CIDRs or IP addresses")

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

func resolveSMTPConfig(opts *serverOptions) invite.MailerConfig {
	smtpHost := strings.TrimSpace(opts.smtpHost)
	if smtpHost == "" {
		if v := strings.TrimSpace(os.Getenv("ORBIT_SMTP_HOST")); v != "" {
			smtpHost = v
		} else if v := strings.TrimSpace(os.Getenv("SMTP_HOST")); v != "" {
			smtpHost = v
		} else {
			smtpHost = "mail.manova.space"
		}
	}

	smtpPort := strings.TrimSpace(opts.smtpPort)
	if smtpPort == "" {
		if v := strings.TrimSpace(os.Getenv("ORBIT_SMTP_PORT")); v != "" {
			smtpPort = v
		} else if v := strings.TrimSpace(os.Getenv("SMTP_PORT")); v != "" {
			smtpPort = v
		} else {
			smtpPort = "587"
		}
	}

	smtpUser := strings.TrimSpace(opts.smtpUser)
	if smtpUser == "" {
		if v := strings.TrimSpace(os.Getenv("ORBIT_SMTP_USER")); v != "" {
			smtpUser = v
		} else if v := strings.TrimSpace(os.Getenv("SMTP_USER")); v != "" {
			smtpUser = v
		}
	}

	smtpPass := strings.TrimSpace(opts.smtpPass)
	if smtpPass == "" {
		if v := strings.TrimSpace(os.Getenv("ORBIT_SMTP_PASS")); v != "" {
			smtpPass = v
		} else if v := strings.TrimSpace(os.Getenv("SMTP_PASS")); v != "" {
			smtpPass = v
		}
	}

	smtpFrom := strings.TrimSpace(opts.smtpFrom)
	if smtpFrom == "" {
		if v := strings.TrimSpace(os.Getenv("ORBIT_SMTP_FROM")); v != "" {
			smtpFrom = v
		} else if v := strings.TrimSpace(os.Getenv("SMTP_FROM")); v != "" {
			smtpFrom = v
		} else {
			smtpFrom = "Orbit Platform <noreply@manova.space>"
		}
	}

	return invite.MailerConfig{
		Host: smtpHost,
		Port: smtpPort,
		User: smtpUser,
		Pass: smtpPass,
		From: smtpFrom,
	}
}

func runServer(cmd *cobra.Command, opts *serverOptions) error {
	out := cmd.OutOrStdout()

	// 1. Resolve workstation configuration from file
	_, _ = config.Resolve(config.ResolveOptions{
		ConfigPath: opts.configPath,
	})

	// 2. Resolve SMTP parameters independently (CLI Flags -> Env Vars -> Defaults)
	smtpCfg := resolveSMTPConfig(opts)
	mailer := invite.NewSMTPMailer(smtpCfg)

	// 3. Resolve signing secret
	secret, secretSource := resolveSigningSecret(opts.signingSecret, opts.ownerStorePath)

	// 4. Resolve and Initialize SQLite Database
	dbPath := opts.dbPath
	if dbPath == "" {
		dbPath = DefaultDBPath()
	}

	db, err := sqlite.NewDB(dbPath)
	if err != nil {
		slog.Error("failed to initialize SQLite database", "path", dbPath, "error", err)
		return fmt.Errorf("failed to open sqlite database at %s: %w", dbPath, err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("error closing sqlite database", "error", err)
		}
	}()

	// 5. Initialize persistent challenge and grant managers
	challengeMgr := owner.NewPersistentChallengeManager(db.Challenges())
	grantMgr := owner.NewPersistentGrantManager(db.Grants())

	// 7. Resolve trusted proxies
	trustedProxies := opts.trustedProxies
	if len(trustedProxies) == 0 {
		if envProxies := os.Getenv("ORBIT_TRUSTED_PROXIES"); envProxies != "" {
			for _, p := range strings.Split(envProxies, ",") {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					trustedProxies = append(trustedProxies, trimmed)
				}
			}
		}
	}

	// 8. Initialize Onboard Server
	serverCfg := onboard.ServerConfig{
		Addr:                    opts.addr,
		Secret:                  []byte(secret),
		Provisioner:             provisioner.NewDevProvisioner(),
		Store:                   db,
		RateLimitStore:          db.RateLimits(),
		ChallengeManager:        challengeMgr,
		GrantManager:            grantMgr,
		Mailer:                  mailer,
		DisablePublicChallenges: opts.disablePublicChallenges,
		TrustedProxies:          trustedProxies,
		Logger:                  slog.Default(),
	}

	srv, err := onboard.NewServer(serverCfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// 9. Start HTTP Listener
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
	fmt.Fprintf(out, "  %s  SQLite database: %s\n", iconOK, subtleStyle.Render(dbPath))
	fmt.Fprintf(out, "  %s  Mail gateway: %s\n", iconOK, subtleStyle.Render(smtpCfg.Host+":"+smtpCfg.Port))
	if secretSource != "" {
		fmt.Fprintf(out, "  %s  Signing secret: %s\n\n", iconOK, infoStyle.Render(secretSource))
	}

	// 10. Graceful Shutdown on SIGINT/SIGTERM or Context Cancellation
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

	if val := strings.TrimSpace(os.Getenv("ORBIT_SIGNING_SECRET")); val != "" {
		return val, "loaded from $ORBIT_SIGNING_SECRET"
	}

	ownerStore := owner.NewStore(ownerStorePath)
	if rec, err := ownerStore.LoadOwner(); err == nil && rec != nil && rec.RootSigningSecret != "" {
		return rec.RootSigningSecret, fmt.Sprintf("sealed owner vault (%s)", rec.Email)
	}

	return DefaultFallbackSecret, "development fallback secret (INSECURE)"
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
