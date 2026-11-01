package main

import (
	"io"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/output"
	updaterapp "github.com/manovaspace/orbit-cli/pkg/updater/application"
	"github.com/spf13/cobra"
)

func updaterUI() updaterapp.UI {
	return updaterapp.UI{
		OK:    iconOK,
		Info:  iconInfo,
		Error: iconError,
		Warn:  iconWarn,
		Arrow: iconArrow,

		Title:   func(text string) string { return titleStyle.Render(text) },
		Header:  func(text string) string { return headerStyle.Render(text) },
		Bold:    func(text string) string { return boldStyle.Render(text) },
		Success: func(text string) string { return successStyle.Render(text) },
		Warning: func(text string) string { return warningStyle.Render(text) },
		Subtle:  func(text string) string { return subtleStyle.Render(text) },
		Code:    func(text string) string { return codeStyle.Render(text) },
	}
}

func newSelfUpdateCmd() *cobra.Command {
	var (
		checkOnly bool
		yesFlag   bool
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:     "self-update",
		Aliases: []string{"selfupdate"},
		Short:   "Update the Orbit CLI binary to the latest release",
		Long:    "Checks GitHub releases for the latest Orbit CLI binary, displays release highlights, and performs atomic binary replacement.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			execPath, _ := os.Executable()
			if execPath == "" {
				execPath = "/usr/local/bin/orbit"
			}

			svc := &updaterapp.Service{Updater: releaseUpdater, Out: uiOut, UI: updaterUI()}
			res, err := svc.SelfUpdate(cmd.Context(), updaterapp.SelfUpdateInput{
				CurrentVersion: version,
				APIURL:         os.Getenv("ORBIT_RELEASE_API_URL"),
				CheckOnly:      checkOnly,
				Yes:            yesFlag,
				ExecutablePath: execPath,
				Confirm: func(question string, defaultYes bool) bool {
					return promptConfirm(in, uiOut, question, defaultYes)
				},
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for updates without downloading or installing")
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Automatically accept update confirmation prompt")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}
