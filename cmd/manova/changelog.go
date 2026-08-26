package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/changelog"
	"github.com/spf13/cobra"
)

func newChangelogCmd() *cobra.Command {
	var (
		targetVersion string
		limit         int
		jsonOut       bool
		noPager       bool
	)

	cmd := &cobra.Command{
		Use:     "changelog",
		Aliases: []string{"whatsnew", "whatisnew", "news"},
		Short:   "View recent release notes and what's new in Manova",
		Long: `Displays release highlights and feature changelogs in beautifully
formatted cards. Output is paged automatically when run in a terminal
(press q to quit, / to search). Use --no-pager to disable paging.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			releases := changelog.FetchReleases(cmd.Context(), "")

			// ── JSON mode — always flat, no pager ──────────────────────
			if jsonOut {
				recent := releases
				if targetVersion != "" {
					rel := changelog.FindRelease(releases, targetVersion)
					if rel == nil {
						return fmt.Errorf("release notes for version %q not found", targetVersion)
					}
					enc := json.NewEncoder(out)
					enc.SetIndent("", "  ")
					return enc.Encode(rel)
				}
				recent = changelog.GetRecentReleases(releases, limit)
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(recent)
			}

			// ── Single version mode ─────────────────────────────────────
			if targetVersion != "" {
				rel := changelog.FindRelease(releases, targetVersion)
				if rel == nil {
					return fmt.Errorf("release notes for version %q not found", targetVersion)
				}
				content := fmt.Sprintf("%s\n\n%s\n",
					titleStyle.Render(fmt.Sprintf("Manova Release Notes — %s", rel.Version)),
					changelog.FormatReleaseCard(*rel),
				)
				if noPager {
					fmt.Fprint(out, content)
					return nil
				}
				outFile, _ := out.(*os.File)
				if outFile == nil {
					outFile = os.Stdout
				}
				return changelog.RunPager(outFile, content)
			}

			// ── Full changelog list ────────────────────────────────────
			recent := changelog.GetRecentReleases(releases, limit)
			content := fmt.Sprintf("%s\n\n%s\n",
				titleStyle.Render("Manova Changelog & Release Notes"),
				changelog.FormatAllCards(recent),
			)

			if len(recent) < len(releases) {
				tip := fmt.Sprintf("\n💡 Tip: Showing %d latest releases. Run 'm changelog -n 10' for more, or 'm whatsnew -v <tag>' for a specific version.", len(recent))
				content += subtleStyle.Render(tip) + "\n"
			}

			if noPager {
				fmt.Fprint(out, content)
				return nil
			}
			outFile, _ := out.(*os.File)
			if outFile == nil {
				outFile = os.Stdout
			}
			return changelog.RunPager(outFile, content)
		},
	}

	cmd.Flags().StringVarP(&targetVersion, "version", "v", "", "Show notes for a specific version tag")
	cmd.Flags().IntVarP(&limit, "limit", "n", 2, "Number of recent releases to display")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output in JSON format (implies --no-pager)")
	cmd.Flags().BoolVar(&noPager, "no-pager", false, "Print directly to stdout without paging")

	return cmd
}
