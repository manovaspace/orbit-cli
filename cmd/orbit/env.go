package main

import (
	"io"
	"path/filepath"

	envapp "github.com/manovaspace/orbit-cli/pkg/env/application"
	"github.com/manovaspace/orbit-cli/pkg/env/infrastructure/mcpenv"
	envosfs "github.com/manovaspace/orbit-cli/pkg/env/infrastructure/osfs"
	"github.com/manovaspace/orbit-cli/pkg/env/infrastructure/schemafile"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/spf13/cobra"
)

// The env commands reach the filesystem, schema loader and MCP setup through
// these seams so tests can swap the adapters.
var (
	envFS      envapp.FS           = envosfs.New()
	envSchemas envapp.SchemaFinder = envosfs.New()
	envLoader  envapp.SchemaLoader = schemafile.New()
	envMCP     envapp.MCPEnv       = mcpenv.New()
)

func newEnvService(out io.Writer) *envapp.Service {
	return &envapp.Service{
		FS:      envFS,
		Schemas: envSchemas,
		Loader:  envLoader,
		MCP:     envMCP,
		Out:     out,
		UI: envapp.UI{
			Title:     func(text string) string { return titleStyle.Render(text) },
			Bold:      func(text string) string { return boldStyle.Render(text) },
			OK:        iconOK,
			Warn:      iconWarn,
			Error:     iconError,
			Success:   func(text string) string { return successStyle.Render(text) },
			Warning:   func(text string) string { return warningStyle.Render(text) },
			ErrorText: func(text string) string { return errorStyle.Render(text) },
			Subtle:    func(text string) string { return subtleStyle.Render(text) },
			Info:      func(text string) string { return infoStyle.Render(text) },
		},
	}
}

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage and validate workspace environment variables and .env files",
		Long:  "Validate project .env files against .env.schema.yaml contracts, generate initial .env files, and manage MCP secrets.",
	}

	cmd.AddCommand(newEnvCheckCmd())
	cmd.AddCommand(newEnvSetupCmd())

	return cmd
}

func envTargetPath(args []string, workspaceRoot string) string {
	targetPath := workspaceRoot
	if len(args) > 0 && args[0] != "" {
		targetPath = args[0]
		if !filepath.IsAbs(targetPath) {
			targetPath = filepath.Join(workspaceRoot, targetPath)
		}
	}
	return targetPath
}

func newEnvCheckCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Validate project .env files against their .env.schema.yaml contracts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			workspaceRoot := findWorkspaceRoot("")
			result, err := newEnvService(uiOut).Check(cmd.Context(), envapp.CheckInput{
				SearchPath:    envTargetPath(args, workspaceRoot),
				WorkspaceRoot: workspaceRoot,
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(result))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newEnvSetupCmd() *cobra.Command {
	var force bool
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "setup [path]",
		Short: "Generate .env files from schemas and initialize .cursor/mcp.env",
		Long:  "Scans for .env.schema.yaml contracts and generates .env files populated with default values and strict 0600 permissions.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			workspaceRoot := findWorkspaceRoot("")
			result, err := newEnvService(uiOut).Setup(cmd.Context(), envapp.SetupInput{
				SearchPath:    envTargetPath(args, workspaceRoot),
				WorkspaceRoot: workspaceRoot,
				Force:         force,
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(result))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing .env files with schema defaults")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}
