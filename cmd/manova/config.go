package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configFlag string

func getConfigPath(cmd *cobra.Command) string {
	if cmd != nil {
		if p, err := cmd.Flags().GetString("config"); err == nil && p != "" {
			return p
		}
	}
	if configFlag != "" {
		return configFlag
	}
	return config.DefaultConfigPath()
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Orbit CLI configuration",
		Long: `View, update, and manage persistent Orbit CLI configuration (~/.config/orbit/config.yaml).
Supports hierarchical resolution with environment variables and command-line flags.`,
	}

	cmd.PersistentFlags().StringVar(&configFlag, "config", "", "Custom path to configuration file")

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigPathCmd())

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	var formatFlag string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Display current configuration with secrets masked",
		Long:  "Displays configuration values loaded from ~/.config/orbit/config.yaml, with sensitive credentials masked.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			cfg, err := config.Load(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					cfg = config.DefaultConfig()
				} else {
					return fmt.Errorf("failed to load configuration: %w", err)
				}
			}

			masked := cfg.Masked()

			if strings.ToLower(formatFlag) == "json" {
				data, err := json.MarshalIndent(masked, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to encode configuration JSON: %w", err)
				}
				fmt.Fprintln(out, string(data))
				return nil
			}

			data, err := yaml.Marshal(masked)
			if err != nil {
				return fmt.Errorf("failed to encode configuration YAML: %w", err)
			}
			fmt.Fprint(out, string(data))
			return nil
		},
	}

	cmd.Flags().StringVarP(&formatFlag, "format", "f", "yaml", "Output format: yaml or json")
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get configuration value for a key",
		Long:  "Retrieve a specific configuration setting by key (e.g. 'server.url', 'admin.email', 'smtp.host', 'smtp.port').",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			cfg, err := config.Load(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					cfg = config.DefaultConfig()
				} else {
					return fmt.Errorf("failed to load configuration: %w", err)
				}
			}

			val, err := cfg.Get(args[0])
			if err != nil {
				return err
			}

			fmt.Fprintln(out, val)
			return nil
		},
	}

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration key-value pair",
		Long:  "Update a specific configuration setting by key and persist it to the configuration file.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			cfg, err := config.Load(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					cfg = config.DefaultConfig()
				} else {
					return fmt.Errorf("failed to load configuration: %w", err)
				}
			}

			key := args[0]
			val := args[1]

			if err := cfg.Set(key, val); err != nil {
				return err
			}

			if err := cfg.Save(cfgPath); err != nil {
				return fmt.Errorf("failed to save configuration file %q: %w", cfgPath, err)
			}

			displayVal := val
			if strings.ToLower(strings.TrimSpace(key)) == "smtp.pass" && val != "" {
				displayVal = "********"
			}

			fmt.Fprintf(out, "%s Set %s = %s (%s)\n", iconOK, boldStyle.Render(key), codeStyle.Render(displayVal), subtleStyle.Render(cfgPath))
			return nil
		},
	}

	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var forceFlag bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize default configuration file",
		Long:  "Creates the Orbit CLI configuration file (~/.config/orbit/config.yaml) with platform defaults.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			if _, err := os.Stat(cfgPath); err == nil && !forceFlag {
				fmt.Fprintf(out, "%s Configuration file already exists at %s (use --force to overwrite)\n",
					iconInfo,
					subtleStyle.Render(cfgPath),
				)
				return nil
			}

			cfg := config.DefaultConfig()
			if err := cfg.Save(cfgPath); err != nil {
				return fmt.Errorf("failed to initialize configuration file %q: %w", cfgPath, err)
			}

			fmt.Fprintf(out, "%s Configuration file initialized at %s (mode 0600)\n",
				iconOK,
				boldStyle.Render(cfgPath),
			)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Overwrite existing configuration file with default values")
	return cmd
}

func newConfigPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print path to the configuration file",
		Long:  "Outputs the active filesystem path to the Orbit CLI configuration file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)
			fmt.Fprintln(out, cfgPath)
			return nil
		},
	}

	return cmd
}
