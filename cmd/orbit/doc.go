package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/manpage"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newDocCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Generate documentation (man pages, markdown)",
		Long:  "Commands for generating Unix manual pages and markdown documentation for the Orbit CLI.",
	}

	manCmd := &cobra.Command{
		Use:   "man [target-dir]",
		Short: "Generate Unix troff man pages (section 1)",
		Long:  "Generates man pages for all commands into target-dir (default: auto-detected man directory).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root := cmd.Root()

			targetDir := ""
			if len(args) > 0 && args[0] != "" {
				targetDir = args[0]
			} else {
				targetDir = manpage.ResolveManDir()
			}

			files, err := manpage.GenerateManPages(root, targetDir)
			if err != nil {
				return fmt.Errorf("failed to generate man pages: %w", err)
			}

			fmt.Fprintf(out, "%s Generated %d man page(s) in %s\n", iconOK, len(files), codeStyle.Render(targetDir))
			for _, f := range files {
				fmt.Fprintf(out, "  • %s\n", subtleStyle.Render(f))
			}
			return nil
		},
	}

	mdCmd := &cobra.Command{
		Use:   "markdown [target-dir]",
		Short: "Generate Markdown documentation",
		Long:  "Generates markdown documentation for all commands into target-dir (default: docs/cli).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root := cmd.Root()

			targetDir := "docs/cli"
			if len(args) > 0 && args[0] != "" {
				targetDir = args[0]
			}

			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %q: %w", targetDir, err)
			}

			if err := doc.GenMarkdownTree(root, targetDir); err != nil {
				return fmt.Errorf("failed to generate markdown documentation: %w", err)
			}

			fmt.Fprintf(out, "%s Generated markdown documentation in %s\n", iconOK, codeStyle.Render(targetDir))
			return nil
		},
	}

	cmd.AddCommand(manCmd)
	cmd.AddCommand(mdCmd)

	return cmd
}
