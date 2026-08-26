package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/manovaspace/orbit-cli/pkg/env"
	"github.com/manovaspace/orbit-cli/pkg/migrate"
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Validate and manage workspace environment variables",
		Long:  "Validate project .env files against .env.schema.yaml contracts, generate initial .env files, and manage MCP secrets.",
	}

	cmd.AddCommand(newEnvCheckCmd())
	cmd.AddCommand(newEnvSetupCmd())

	return cmd
}

func findSchemaFiles(searchRoot string) []string {
	var schemas []string

	_ = filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "temp" || name == ".trash" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		if d.Name() == ".env.schema.yaml" || d.Name() == ".env.schema.yml" {
			schemas = append(schemas, path)
		}

		return nil
	})

	sort.Strings(schemas)
	return schemas
}

func newEnvCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Validate project .env files against their .env.schema.yaml contracts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			workspaceRoot := findWorkspaceRoot("")

			targetPath := workspaceRoot
			if len(args) > 0 && args[0] != "" {
				targetPath = args[0]
				if !filepath.IsAbs(targetPath) {
					targetPath = filepath.Join(workspaceRoot, targetPath)
				}
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit Environment Validator"))
			fmt.Fprintf(out, "  Search Path: %s\n\n", subtleStyle.Render(targetPath))

			schemas := findSchemaFiles(targetPath)
			if len(schemas) == 0 {
				fmt.Fprintln(out, subtleStyle.Render("No .env.schema.yaml contracts found in target path."))
				return nil
			}

			totalErrors := 0
			validCount := 0

			for _, schemaPath := range schemas {
				dir := filepath.Dir(schemaPath)
				relDir, _ := filepath.Rel(workspaceRoot, dir)
				if relDir == "" || relDir == "." {
					relDir = filepath.Base(dir)
				}

				schema, err := env.LoadSchema(schemaPath)
				if err != nil {
					fmt.Fprintf(out, "  %s  %s: failed to load schema: %v\n", iconError, boldStyle.Render(relDir), err)
					totalErrors++
					continue
				}

				envPath := filepath.Join(dir, ".env")
				valErrors := env.Validate(envPath, schema)

				if len(valErrors) == 0 {
					validCount++
					fmt.Fprintf(out, "  %s  %s: %s (%d variables verified)\n",
						iconOK,
						boldStyle.Render(relDir),
						successStyle.Render(".env valid"),
						len(schema.Variables),
					)
				} else {
					totalErrors += len(valErrors)
					fmt.Fprintf(out, "  %s  %s: %s\n",
						iconError,
						boldStyle.Render(relDir),
						errorStyle.Render(fmt.Sprintf("%d validation issue(s)", len(valErrors))),
					)
					for _, ve := range valErrors {
						var badge string
						if ve.Type == "missing_required" || ve.Type == "file_not_found" {
							badge = errorStyle.Render("[MISSING]")
						} else {
							badge = warningStyle.Render("[INVALID]")
						}
						if ve.Variable != "" {
							fmt.Fprintf(out, "       %s %s: %s\n", badge, boldStyle.Render(ve.Variable), ve.Message)
						} else {
							fmt.Fprintf(out, "       %s %s\n", badge, ve.Message)
						}
					}
				}
			}

			// Check .cursor/mcp.env if in workspaceRoot
			mcpEnvPath := filepath.Join(workspaceRoot, ".cursor", "mcp.env")
			if fi, err := os.Stat(mcpEnvPath); err == nil {
				perm := fi.Mode().Perm()
				if perm != 0600 {
					fmt.Fprintf(out, "\n  %s  %s: permissions are %04o (recommended: 0600)\n",
						iconWarn,
						boldStyle.Render(".cursor/mcp.env"),
						perm,
					)
				} else {
					fmt.Fprintf(out, "\n  %s  %s: permissions 0600 secure\n",
						iconOK,
						boldStyle.Render(".cursor/mcp.env"),
					)
				}
			}

			// Summary
			fmt.Fprintf(out, "\n%s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d schemas valid", validCount)),
				errorStyle.Render(fmt.Sprintf("✖ %d errors across all env files", totalErrors)),
			)

			if totalErrors > 0 {
				return fmt.Errorf("environment validation failed with %d error(s)", totalErrors)
			}

			return nil
		},
	}

	return cmd
}

func newEnvSetupCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "setup [path]",
		Short: "Generate .env files from schemas and initialize .cursor/mcp.env",
		Long:  "Scans for .env.schema.yaml contracts and generates .env files populated with default values and strict 0600 permissions.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			workspaceRoot := findWorkspaceRoot("")

			targetPath := workspaceRoot
			if len(args) > 0 && args[0] != "" {
				targetPath = args[0]
				if !filepath.IsAbs(targetPath) {
					targetPath = filepath.Join(workspaceRoot, targetPath)
				}
			}

			fmt.Fprintln(out, titleStyle.Render("Orbit Environment Setup"))

			schemas := findSchemaFiles(targetPath)
			createdCount := 0
			skippedCount := 0

			for _, schemaPath := range schemas {
				dir := filepath.Dir(schemaPath)
				relDir, _ := filepath.Rel(workspaceRoot, dir)
				if relDir == "" || relDir == "." {
					relDir = filepath.Base(dir)
				}

				envPath := filepath.Join(dir, ".env")
				if _, err := os.Stat(envPath); err == nil && !force {
					skippedCount++
					fmt.Fprintf(out, "  %s  %s: %s\n", iconOK, boldStyle.Render(relDir), subtleStyle.Render(".env already exists (use --force to overwrite)"))
					continue
				}

				schema, err := env.LoadSchema(schemaPath)
				if err != nil {
					fmt.Fprintf(out, "  %s  %s: error reading schema: %v\n", iconError, boldStyle.Render(relDir), err)
					continue
				}

				if err := env.GenerateFromSchema(envPath, schema, nil); err != nil {
					fmt.Fprintf(out, "  %s  %s: generation failed: %v\n", iconError, boldStyle.Render(relDir), err)
				} else {
					createdCount++
					fmt.Fprintf(out, "  %s  %s: %s\n", iconOK, boldStyle.Render(relDir), successStyle.Render("generated .env (chmod 0600)"))
				}
			}

			// Ensure .cursor/mcp.env
			if err := migrate.SetupMCPEnvironment(workspaceRoot); err != nil {
				fmt.Fprintf(out, "  %s  .cursor/mcp.env setup warning: %v\n", iconWarn, err)
			} else {
				fmt.Fprintf(out, "  %s  %s: %s\n", iconOK, boldStyle.Render(".cursor/mcp.env"), successStyle.Render("ready (chmod 0600)"))
			}

			fmt.Fprintf(out, "\n%s  %s\n",
				successStyle.Render(fmt.Sprintf("✔ %d .env files generated", createdCount)),
				infoStyle.Render(fmt.Sprintf("ℹ %d already existed", skippedCount)),
			)

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing .env files with schema defaults")

	return cmd
}
