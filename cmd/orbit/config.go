package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/config"
	"github.com/manovaspace/orbit-cli/pkg/table"
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

func isSecretKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "pass") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "key") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "auth")
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
	cmd.AddCommand(newConfigUnsetCmd())
	cmd.AddCommand(newConfigListCmd())
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

			res, err := config.Resolve(config.ResolveOptions{ConfigPath: cfgPath})
			if err != nil {
				return fmt.Errorf("failed to resolve configuration: %w", err)
			}

			masked := res.Config.Masked()

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
	var rawFlag bool

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get configuration value for a key",
		Long:  "Retrieve a specific configuration setting by key (e.g. 'server.url', 'defaults.scope').",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			res, err := config.Resolve(config.ResolveOptions{ConfigPath: cfgPath})
			if err != nil {
				return err
			}

			val, err := res.Config.Get(args[0])
			if err != nil {
				return err
			}

			if rawFlag {
				fmt.Fprint(out, val.Value)
			} else {
				fmt.Fprintln(out, val.Value)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&rawFlag, "raw", false, "Print raw value without trailing newline or formatting")
	return cmd
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration key-value pair",
		Long:  "Update a specific configuration setting by key and persist it using comment-preserving AST yaml.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			testCfg, err := config.Load(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					testCfg = config.DefaultConfig()
				} else {
					return fmt.Errorf("failed to load configuration: %w", err)
				}
			}

			key := args[0]
			val := args[1]

			if err := testCfg.Set(key, val); err != nil {
				return err
			}

			if isSecretKey(key) {
				fmt.Fprintf(out, "%s Warning: '%s' resembles a secret. Storing secrets in config.yaml is discouraged.\n", iconWarn, key)
			}

			if err := config.SetInFile(cfgPath, key, val); err != nil {
				return fmt.Errorf("failed to persist configuration: %w", err)
			}

			fmt.Fprintf(out, "%s Set %s = %s (%s)\n", iconOK, boldStyle.Render(key), codeStyle.Render(val), subtleStyle.Render(cfgPath))
			return nil
		},
	}

	return cmd
}

func newConfigUnsetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a configuration key",
		Long:  "Remove a custom key or reset a core domain property to default in the configuration file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			testCfg, err := config.Load(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					testCfg = config.DefaultConfig()
				} else {
					return fmt.Errorf("failed to load configuration: %w", err)
				}
			}

			key := strings.TrimSpace(args[0])
			if key == "" {
				return errors.New("key cannot be empty")
			}

			if err := testCfg.Unset(key); err != nil {
				return err
			}

			if err := config.UnsetInFile(cfgPath, key); err != nil {
				return fmt.Errorf("failed to unset %s in %q: %w", key, cfgPath, err)
			}

			fmt.Fprintf(out, "%s Unset %s (%s)\n", iconOK, boldStyle.Render(key), subtleStyle.Render(cfgPath))
			return nil
		},
	}

	return cmd
}

func formatSource(e config.ConfigEntry) string {
	switch e.Source {
	case config.SourceDefault:
		return subtleStyle.Render("default")
	case config.SourceUserFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("user-config (%s)", subtleStyle.Render(e.SourceRef))
		}
		return "user-config"
	case config.SourceWorkFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("workspace-config (%s)", subtleStyle.Render(e.SourceRef))
		}
		return "workspace-config"
	case config.SourceEnv:
		if e.SourceRef != "" {
			return fmt.Sprintf("env (%s)", codeStyle.Render(e.SourceRef))
		}
		return "env"
	case config.SourceFlag:
		if e.SourceRef != "" {
			return fmt.Sprintf("flag (%s)", boldStyle.Render(e.SourceRef))
		}
		return "flag"
	default:
		if e.SourceRef != "" {
			return fmt.Sprintf("%s (%s)", string(e.Source), e.SourceRef)
		}
		return string(e.Source)
	}
}

func formatSourcePlainText(e config.ConfigEntry) string {
	switch e.Source {
	case config.SourceDefault:
		return "default"
	case config.SourceUserFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("user-config (%s)", e.SourceRef)
		}
		return "user-config"
	case config.SourceWorkFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("workspace-config (%s)", e.SourceRef)
		}
		return "workspace-config"
	case config.SourceEnv:
		if e.SourceRef != "" {
			return fmt.Sprintf("env (%s)", e.SourceRef)
		}
		return "env"
	case config.SourceFlag:
		if e.SourceRef != "" {
			return fmt.Sprintf("flag (%s)", e.SourceRef)
		}
		return "flag"
	default:
		if e.SourceRef != "" {
			return fmt.Sprintf("%s (%s)", string(e.Source), e.SourceRef)
		}
		return string(e.Source)
	}
}

func newConfigListCmd() *cobra.Command {
	var (
		formatFlag string
		pageFlag   int
		limitFlag  int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all resolved configuration entries and their sources",
		Long:  "Displays all active configuration parameters across defaults, configuration file, environment variables, and flags.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			res, err := config.Resolve(config.ResolveOptions{ConfigPath: cfgPath})
			if err != nil {
				return fmt.Errorf("failed to resolve configuration: %w", err)
			}

			entries := res.ListEntries()

			if strings.ToLower(formatFlag) == "json" {
				data, err := json.MarshalIndent(entries, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to encode entries JSON: %w", err)
				}
				fmt.Fprintln(out, string(data))
				return nil
			}

			if strings.ToLower(formatFlag) == "yaml" {
				data, err := yaml.Marshal(entries)
				if err != nil {
					return fmt.Errorf("failed to encode entries YAML: %w", err)
				}
				fmt.Fprint(out, string(data))
				return nil
			}

			// Table format
			tbl := table.New(
				table.Column{Title: "KEY", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 18},
				table.Column{Title: "VALUE", HeaderStyle: headerStyle, CellStyle: codeStyle, MinWidth: 20, Flexible: true},
				table.Column{Title: "TYPE", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 8},
				table.Column{Title: "SOURCE", HeaderStyle: headerStyle, MinWidth: 14, Flexible: false},
			)

			for _, e := range entries {
				kCell := table.PlainCell(e.Key)
				vCell := table.PlainCell(e.Value)
				tCell := table.PlainCell(e.Type)
				sCell := table.StyledCell(formatSourcePlainText(e), formatSource(e))

				tbl.AddStyledRow(kCell, vCell, tCell, sCell)
			}

			if limitFlag > 0 {
				tbl.WithPagination(pageFlag, limitFlag)
			}

			fmt.Fprintln(out)
			_ = tbl.Render(out)

			return nil
		},
	}

	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format: table, json, or yaml")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum rows per page (0 = all)")
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
