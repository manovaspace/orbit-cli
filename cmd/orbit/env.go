package main

import (
	"fmt"
	"io"
	"path/filepath"

	envapp "github.com/manovaspace/orbit-cli/pkg/env/application"
	"github.com/manovaspace/orbit-cli/pkg/env/infrastructure/mcpenv"
	envosfs "github.com/manovaspace/orbit-cli/pkg/env/infrastructure/osfs"
	"github.com/manovaspace/orbit-cli/pkg/env/infrastructure/schemafile"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
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
			Title:     func(text string) string { return render.TitleLegacy.Render(text) },
			Bold:      func(text string) string { return render.Bold.Render(text) },
			OK:        render.IconOK,
			Warn:      render.IconWarn,
			Error:     render.IconError,
			Success:   func(text string) string { return render.Success.Render(text) },
			Warning:   func(text string) string { return render.Warning.Render(text) },
			ErrorText: func(text string) string { return render.Error.Render(text) },
			Subtle:    func(text string) string { return render.Subtle.Render(text) },
			Info:      func(text string) string { return render.Info.Render(text) },
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

			workspaceRoot := findWorkspaceRoot("")
			result, err := newEnvService(io.Discard).Check(cmd.Context(), envapp.CheckInput{
				SearchPath:    envTargetPath(args, workspaceRoot),
				WorkspaceRoot: workspaceRoot,
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
					return err
				}
				p := render.NewPrinter(out)
				renderEnvCheck(p, result)
				if perr := p.Err(); perr != nil {
					return perr
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(result))
			}

			p := render.NewPrinter(out)
			renderEnvCheck(p, result)
			return p.Err()
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

// renderEnvCheck writes one section per contract plus the MCP permission state
// and the shared summary line.
func renderEnvCheck(p *render.Printer, res envapp.CheckResult) {
	p.Title("Environment contracts")
	p.KV("Search path", res.SearchPath)

	for _, schema := range res.Schemas {
		p.Section(schema.Directory)
		switch {
		case schema.LoadError != "":
			p.Line(render.StatusFail, "failed to load schema: "+schema.LoadError)
		case len(schema.Errors) > 0:
			p.Line(render.StatusFail, fmt.Sprintf("%d validation issue(s)", len(schema.Errors)))
			for _, issue := range schema.Errors {
				status := render.StatusFail
				label := "invalid"
				if issue.Type == "missing_required" || issue.Type == "file_not_found" {
					status = render.StatusWarn
					label = "missing"
				}
				text := label
				if issue.Variable != "" {
					text += " " + issue.Variable
				}
				if issue.Message != "" {
					text += ": " + issue.Message
				}
				p.Line(status, text)
			}
		default:
			p.Line(render.StatusOK, fmt.Sprintf(".env valid (%d variables verified)", schema.VariableCount))
		}
	}
	if len(res.Schemas) == 0 {
		p.Empty(render.Plural(0, "environment contract", "environment contracts"))
	}

	if mcp := res.MCPEnv; mcp != nil {
		p.Section("MCP environment")
		switch {
		case mcp.Exists && mcp.Secure:
			p.Line(render.StatusOK, ".cursor/mcp.env permissions 0600 secure")
		case mcp.Exists:
			p.Line(render.StatusWarn, fmt.Sprintf(".cursor/mcp.env permissions are %04o (recommended: 0600)", mcp.Permissions))
		case mcp.Error != "":
			p.Line(render.StatusFail, "MCP permission inspection failed: "+mcp.Error)
		default:
			p.Line(render.StatusUnverified, ".cursor/mcp.env not present")
		}
	}

	valid, missing, invalid, failed, warnings := 0, 0, 0, 0, 0
	for _, schema := range res.Schemas {
		switch {
		case schema.LoadError != "":
			failed++
		case len(schema.Errors) == 0:
			valid++
		default:
			for _, issue := range schema.Errors {
				if issue.Type == "missing_required" || issue.Type == "file_not_found" {
					missing++
				} else {
					invalid++
				}
			}
		}
	}
	if mcp := res.MCPEnv; mcp != nil {
		switch {
		case mcp.Error != "":
			failed++
		case mcp.Exists && !mcp.Secure:
			warnings++
		}
	}

	unverified := 0
	if !res.Verified {
		unverified++
	}
	if mcp := res.MCPEnv; mcp != nil && !mcp.Exists && mcp.Error == "" {
		unverified++
	}
	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: valid, Label: "valid"},
		render.SummaryPart{Status: render.StatusWarn, Count: missing, Label: "missing"},
		render.SummaryPart{Status: render.StatusFail, Count: invalid, Label: "invalid"},
		render.SummaryPart{Status: render.StatusFail, Count: failed, Label: "failed"},
		render.SummaryPart{Status: render.StatusWarn, Count: warnings, Label: render.Plural(warnings, "warning", "warnings")},
		render.SummaryPart{Status: render.StatusUnverified, Count: unverified, Label: "unverified"},
	)
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
