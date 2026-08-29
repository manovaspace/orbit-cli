package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newDocCmd() *cobra.Command {
	var (
		outputDir string
		format    string
	)

	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Generate documentation and man pages for Orbit CLI",
		Long:  "Generates markdown documentation or unix man pages for all Orbit commands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}

			root := cmd.Root()
			switch format {
			case "man":
				header := &doc.GenManHeader{
					Title:   "ORBIT",
					Section: "1",
					Source:  "Orbit Developer Platform",
					Manual:  "Orbit Platform Manual",
				}
				if err := doc.GenManTree(root, header, outputDir); err != nil {
					return fmt.Errorf("failed to generate man pages: %w", err)
				}
				fmt.Fprintf(out, "  %s  Man pages generated at %s\n", iconOK, codeStyle.Render(outputDir))
			case "markdown", "md":
				if err := doc.GenMarkdownTree(root, outputDir); err != nil {
					return fmt.Errorf("failed to generate markdown docs: %w", err)
				}
				fmt.Fprintf(out, "  %s  Markdown documentation generated at %s\n", iconOK, codeStyle.Render(outputDir))
			default:
				return fmt.Errorf("unsupported format %q, use 'man' or 'markdown'", format)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "./docs/cli", "Directory to output generated documentation (markdown default; use -f man -o docs/cli/man for man pages)")
	cmd.Flags().StringVarP(&format, "format", "f", "markdown", "Documentation format: markdown or man")

	return cmd
}
