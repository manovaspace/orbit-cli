package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultServerURL  = "https://orbit.manova.space"
	DefaultAdminEmail = "alirezaopmc@gmail.com"
	DefaultSMTPHost   = "mail.manova.space"
	DefaultSMTPPort   = 587
	DefaultSMTPFrom   = "Orbit Platform <noreply@manova.space>"
	DefaultScope      = "core"
	DefaultExpiryDays = 7
)

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			URL:     DefaultServerURL,
			Timeout: 15 * time.Second,
		},
		Admin: AdminConfig{
			Email: DefaultAdminEmail,
			Name:  "Alireza",
		},
		SMTP: SMTPConfig{
			Host: DefaultSMTPHost,
			Port: DefaultSMTPPort,
			From: DefaultSMTPFrom,
			TLS:  true,
		},
		Defaults: DefaultsConfig{
			Scope:      DefaultScope,
			ExpiryDays: DefaultExpiryDays,
		},
	}
}

func DefaultConfigPath() string {
	if p := strings.TrimSpace(os.Getenv("ORBIT_CONFIG")); p != "" {
		return p
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "orbit", "config.yaml")
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "orbit", "config.yaml")
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	// Auto-tighten permissions if wider than 0600
	if info.Mode().Perm()&0077 != 0 || info.Mode().Perm() > 0600 {
		_ = os.Chmod(path, 0600)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config yaml: %w", err)
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory %q: %w", dir, err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to encode config yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file %q: %w", path, err)
	}
	_ = os.Chmod(path, 0600)
	return nil
}

func envOrEmpty(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func Resolve(opts ResolveOptions) (*Config, error) {
	cfg := DefaultConfig()

	// Load from file if exists
	path := opts.ConfigPath
	if path == "" {
		path = DefaultConfigPath()
	}

	if loaded, err := Load(path); err == nil && loaded != nil {
		cfg = loaded
	}

	// Environment variable overrides (ORBIT_* only)
	if val := envOrEmpty("ORBIT_SERVER"); val != "" {
		cfg.Server.URL = val
	}
	if val := envOrEmpty("ORBIT_ADMIN_EMAIL"); val != "" {
		cfg.Admin.Email = val
	}
	if val := envOrEmpty("ORBIT_ADMIN_NAME"); val != "" {
		cfg.Admin.Name = val
	}
	if val := envOrEmpty("ORBIT_SMTP_HOST"); val != "" {
		cfg.SMTP.Host = val
	}
	if val := envOrEmpty("ORBIT_SMTP_PORT"); val != "" {
		if p, err := strconv.Atoi(val); err == nil && p > 0 {
			cfg.SMTP.Port = p
		}
	}
	if val := envOrEmpty("ORBIT_SMTP_USER"); val != "" {
		cfg.SMTP.User = val
	}
	if val := envOrEmpty("ORBIT_SMTP_PASS"); val != "" {
		cfg.SMTP.Pass = val
	}
	if val := envOrEmpty("ORBIT_SMTP_FROM"); val != "" {
		cfg.SMTP.From = val
	}

	// CLI flag overrides
	if opts.ServerFlag != "" {
		cfg.Server.URL = opts.ServerFlag
	}
	if opts.OwnerFlag != "" {
		cfg.Admin.Email = opts.OwnerFlag
	}
	if opts.NameFlag != "" {
		cfg.Admin.Name = opts.NameFlag
	}
	if opts.SMTPHost != "" {
		cfg.SMTP.Host = opts.SMTPHost
	}
	if opts.SMTPPort > 0 {
		cfg.SMTP.Port = opts.SMTPPort
	}
	if opts.SMTPUser != "" {
		cfg.SMTP.User = opts.SMTPUser
	}
	if opts.SMTPPass != "" {
		cfg.SMTP.Pass = opts.SMTPPass
	}
	if opts.SMTPFrom != "" {
		cfg.SMTP.From = opts.SMTPFrom
	}

	return cfg, nil
}

func (c *Config) Get(key string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "server.url", "server":
		return c.Server.URL, nil
	case "admin.email", "admin", "owner":
		return c.Admin.Email, nil
	case "admin.name":
		return c.Admin.Name, nil
	case "smtp.host":
		return c.SMTP.Host, nil
	case "smtp.port":
		return strconv.Itoa(c.SMTP.Port), nil
	case "smtp.user":
		return c.SMTP.User, nil
	case "smtp.pass":
		return c.SMTP.Pass, nil
	case "smtp.from":
		return c.SMTP.From, nil
	case "defaults.scope":
		return c.Defaults.Scope, nil
	case "defaults.expiry_days", "defaults.expirydays":
		return strconv.Itoa(c.Defaults.ExpiryDays), nil
	default:
		return "", fmt.Errorf("unknown configuration key %q", key)
	}
}

func (c *Config) Set(key, value string) error {
	v := strings.TrimSpace(value)
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "server.url", "server":
		c.Server.URL = v
	case "admin.email", "admin", "owner":
		c.Admin.Email = v
	case "admin.name":
		c.Admin.Name = v
	case "smtp.host":
		c.SMTP.Host = v
	case "smtp.port":
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 {
			return fmt.Errorf("invalid port %q: must be positive integer", v)
		}
		c.SMTP.Port = p
	case "smtp.user":
		c.SMTP.User = v
	case "smtp.pass":
		c.SMTP.Pass = v
	case "smtp.from":
		c.SMTP.From = v
	case "defaults.scope":
		c.Defaults.Scope = v
	case "defaults.expiry_days", "defaults.expirydays":
		days, err := strconv.Atoi(v)
		if err != nil || days <= 0 {
			return fmt.Errorf("invalid expiry_days %q: must be positive integer", v)
		}
		c.Defaults.ExpiryDays = days
	default:
		return fmt.Errorf("unknown configuration key %q", key)
	}
	return nil
}

func (c *Config) Masked() *Config {
	cpy := *c
	if cpy.SMTP.Pass != "" {
		cpy.SMTP.Pass = "********"
	}
	return &cpy
}
