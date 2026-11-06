package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/config/application"
	"github.com/manovaspace/orbit-cli/pkg/config/domain"
	"github.com/manovaspace/orbit-cli/pkg/config/infrastructure/osfile"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configFlag string

// The config commands reach the filesystem and environment through these seams.
var (
	configStore application.Store       = osfile.New()
	configEnv   application.Environment = osfile.NewEnv()
)

func newConfigService(out io.Writer) *application.Service {
	return &application.Service{
		Store: configStore,
		Env:   configEnv,
		Out:   out,
		UI: application.UI{
			OK:   iconOK,
			Warn: iconWarn,
			Info: iconInfo,

			Bold:   func(text string) string { return boldStyle.Render(text) },
			Subtle: func(text string) string { return subtleStyle.Render(text) },
			Code:   func(text string) string { return codeStyle.Render(text) },
		},
	}
}

func getConfigPath(cmd *cobra.Command) string {
	if cmd != nil {
		if p, err := cmd.Flags().GetString("config"); err == nil && p != "" {
			return p
		}
	}
	if configFlag != "" {
		return configFlag
	}
	return configStore.DefaultPath()
}

func writeConfigFailure(out io.Writer, jsonFlag bool, err error) {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
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
	var (
		formatFlag string
		jsonFlag   bool
	)

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Display current configuration with secrets masked",
		Long:  "Displays configuration values loaded from ~/.config/orbit/config.yaml, with sensitive credentials masked.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			masked, err := newConfigService(io.Discard).Show(cfgPath)
			if err != nil {
				writeConfigFailure(out, jsonFlag, err)
				return err
			}

			if jsonFlag {
				return output.Write(out, output.Success(masked))
			}

			p := render.NewPrinter(out)
			if strings.ToLower(formatFlag) == "json" {
				data, err := json.MarshalIndent(masked, "", "  ")
				if err != nil {
					encErr := fmt.Errorf("failed to encode configuration JSON: %w", err)
					writeConfigFailure(out, jsonFlag, encErr)
					return encErr
				}
				p.Text(string(data) + "\n")
				return p.Err()
			}

			data, err := yaml.Marshal(masked)
			if err != nil {
				encErr := fmt.Errorf("failed to encode configuration YAML: %w", err)
				writeConfigFailure(out, jsonFlag, encErr)
				return encErr
			}
			p.Text(string(data))
			return p.Err()
		},
	}

	cmd.Flags().StringVarP(&formatFlag, "format", "f", "yaml", "Output format: yaml or json")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	var (
		rawFlag  bool
		jsonFlag bool
	)

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get configuration value for a key",
		Long:  "Retrieve a specific configuration setting by key (e.g. 'server.url', 'defaults.scope').",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			entry, err := newConfigService(io.Discard).Get(cfgPath, args[0])
			if err != nil {
				writeConfigFailure(out, jsonFlag, err)
				return err
			}

			if jsonFlag {
				return output.Write(out, output.Success(entry))
			}
			p := render.NewPrinter(out)
			if rawFlag {
				p.Text(entry.Value)
			} else {
				p.Text(entry.Value + "\n")
			}
			return p.Err()
		},
	}

	cmd.Flags().BoolVar(&rawFlag, "raw", false, "Print raw value without trailing newline or formatting")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newConfigSetCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration key-value pair",
		Long:  "Update a specific configuration setting by key and persist it using comment-preserving AST yaml.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			cfgPath := getConfigPath(cmd)

			res, err := newConfigService(uiOut).Set(cfgPath, args[0], args[1])
			if err != nil {
				writeConfigFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newConfigUnsetCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a configuration key",
		Long:  "Remove a custom key or reset a core domain property to default in the configuration file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			cfgPath := getConfigPath(cmd)

			res, err := newConfigService(uiOut).Unset(cfgPath, args[0])
			if err != nil {
				writeConfigFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func formatSource(e domain.ConfigEntry) string {
	switch e.Source {
	case domain.SourceDefault:
		return subtleStyle.Render("default")
	case domain.SourceUserFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("user-config (%s)", subtleStyle.Render(e.SourceRef))
		}
		return "user-config"
	case domain.SourceWorkFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("workspace-config (%s)", subtleStyle.Render(e.SourceRef))
		}
		return "workspace-config"
	case domain.SourceEnv:
		if e.SourceRef != "" {
			return fmt.Sprintf("env (%s)", codeStyle.Render(e.SourceRef))
		}
		return "env"
	case domain.SourceFlag:
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

func formatSourcePlainText(e domain.ConfigEntry) string {
	switch e.Source {
	case domain.SourceDefault:
		return "default"
	case domain.SourceUserFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("user-config (%s)", e.SourceRef)
		}
		return "user-config"
	case domain.SourceWorkFile:
		if e.SourceRef != "" {
			return fmt.Sprintf("workspace-config (%s)", e.SourceRef)
		}
		return "workspace-config"
	case domain.SourceEnv:
		if e.SourceRef != "" {
			return fmt.Sprintf("env (%s)", e.SourceRef)
		}
		return "env"
	case domain.SourceFlag:
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
		jsonFlag   bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all resolved configuration entries and their sources",
		Long:  "Displays all active configuration parameters across defaults, configuration file, environment variables, and flags.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)

			entries, err := newConfigService(io.Discard).List(cfgPath)
			if err != nil {
				writeConfigFailure(out, jsonFlag, err)
				return err
			}

			if jsonFlag {
				return output.Write(out, output.Success(entries))
			}

			p := render.NewPrinter(out)
			if strings.ToLower(formatFlag) == "json" {
				data, err := json.MarshalIndent(entries, "", "  ")
				if err != nil {
					encErr := fmt.Errorf("failed to encode entries JSON: %w", err)
					writeConfigFailure(out, jsonFlag, encErr)
					return encErr
				}
				p.Text(string(data) + "\n")
				return p.Err()
			}

			if strings.ToLower(formatFlag) == "yaml" {
				data, err := yaml.Marshal(entries)
				if err != nil {
					encErr := fmt.Errorf("failed to encode entries YAML: %w", err)
					writeConfigFailure(out, jsonFlag, encErr)
					return encErr
				}
				p.Text(string(data))
				return p.Err()
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

			return p.Table(tbl)
		},
	}

	cmd.Flags().StringVarP(&formatFlag, "format", "f", "table", "Output format: table, json, or yaml")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum rows per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var (
		forceFlag bool
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize default configuration file",
		Long:  "Creates the Orbit CLI configuration file (~/.config/orbit/config.yaml) with platform defaults.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			cfgPath := getConfigPath(cmd)

			res, err := newConfigService(uiOut).Init(cfgPath, forceFlag)
			if err != nil {
				writeConfigFailure(out, jsonFlag, err)
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Overwrite existing configuration file with default values")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

type configPathResult struct {
	Path string `json:"path"`
}

func newConfigPathCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print path to the configuration file",
		Long:  "Outputs the active filesystem path to the Orbit CLI configuration file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfgPath := getConfigPath(cmd)
			if jsonFlag {
				return output.Write(out, output.Success(configPathResult{Path: cfgPath}))
			}
			p := render.NewPrinter(out)
			p.Text(cfgPath + "\n")
			return p.Err()
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}
