package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

type docResult struct {
	Format    string `json:"format"`
	OutputDir string `json:"outputDir"`
}

func newDocCmd() *cobra.Command {
	var (
		outputDir string
		format    string
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Generate documentation and man pages for Orbit CLI",
		Long:  "Generates markdown documentation or unix man pages for all Orbit commands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			if err := os.MkdirAll(outputDir, 0755); err != nil {
				docErr := clierr.Wrap(err, clierr.Failed("failed to create output directory"))
				writeDocFailure(out, jsonFlag, docErr)
				return docErr
			}

			root := cmd.Root()
			normalized := format
			switch format {
			case "man":
				header := &doc.GenManHeader{
					Title:   "ORBIT",
					Section: "1",
					Source:  "Orbit Developer Platform",
					Manual:  "Orbit Platform Manual",
				}
				if os.Getenv("SOURCE_DATE_EPOCH") == "" {
					// Keep the original publication date stable across CI regenerations.
					publicationDate := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)
					header.Date = &publicationDate
				}
				if err := doc.GenManTree(root, header, outputDir); err != nil {
					docErr := clierr.Wrap(err, clierr.Failed("failed to generate man pages"))
					writeDocFailure(out, jsonFlag, docErr)
					return docErr
				}
				fmt.Fprintf(uiOut, "  %s  Man pages generated at %s\n", iconOK, codeStyle.Render(outputDir))
			case "markdown", "md":
				normalized = "markdown"
				if err := doc.GenMarkdownTree(root, outputDir); err != nil {
					docErr := clierr.Wrap(err, clierr.Failed("failed to generate markdown docs"))
					writeDocFailure(out, jsonFlag, docErr)
					return docErr
				}
				fmt.Fprintf(uiOut, "  %s  Markdown documentation generated at %s\n", iconOK, codeStyle.Render(outputDir))
			default:
				docErr := clierr.Usage(fmt.Sprintf("unsupported format %q, use 'man' or 'markdown'", format))
				writeDocFailure(out, jsonFlag, docErr)
				return docErr
			}

			if jsonFlag {
				return output.Write(out, output.Success(docResult{Format: normalized, OutputDir: outputDir}))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "./docs/cli", "Directory to output generated documentation (markdown default; use -f man -o docs/cli/man for man pages)")
	cmd.Flags().StringVarP(&format, "format", "f", "markdown", "Documentation format: markdown or man")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

func writeDocFailure(out io.Writer, jsonFlag bool, err error) {
	if jsonFlag {
		_ = output.Write(out, output.Failure(err))
	}
}
