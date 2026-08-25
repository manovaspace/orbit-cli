package main

import (
	"encoding/json"
	"fmt"

	"github.com/manovaspace/orbit-cli/pkg/changelog"
	"github.com/spf13/cobra"
)

func newChangelogCmd() *cobra.Command {
	var (
		targetVersion string
		limit         int
		jsonOut       bool
	)

	cmd := &cobra.Command{
		Use:     "changelog",
		Aliases: []string{"whatsnew", "whatisnew", "news"},
		Short:   "View recent release notes and what's new in Manova",
		Long:    "Displays release highlights, feature changelogs, and version history directly in the terminal.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			releases := changelog.FetchReleases(cmd.Context(), "")

			if targetVersion != "" {
				rel := changelog.FindRelease(releases, targetVersion)
				if rel == nil {
					return fmt.Errorf("release notes for version %q not found", targetVersion)
				}

				if jsonOut {
					enc := json.NewEncoder(out)
					enc.SetIndent("", "  ")
					return enc.Encode(rel)
				}

				fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Manova Release Notes — %s", rel.Version)))
				fmt.Fprintln(out, changelog.FormatReleaseCard(*rel))
				return nil
			}

			recent := changelog.GetRecentReleases(releases, limit)

			if jsonOut {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(recent)
			}

			fmt.Fprintln(out, titleStyle.Render("Manova Changelog & Release Notes"))
			for _, r := range recent {
				fmt.Fprintln(out, changelog.FormatReleaseCard(r))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&targetVersion, "version", "v", "", "Show notes for a specific version tag")
	cmd.Flags().IntVarP(&limit, "limit", "n", 5, "Number of recent releases to display")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output in JSON format")

	return cmd
}
