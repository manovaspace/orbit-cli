package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	prdomain "github.com/manovaspace/orbit-cli/pkg/pr/domain"
	"github.com/manovaspace/orbit-cli/pkg/release"
	"github.com/manovaspace/orbit-cli/pkg/release/application"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/spf13/cobra"
)

// releaseRunner is the seam through which the release commands reach gh, git
// and psql. The gate timings are package-level so command tests run instantly.
var (
	releaseRunner          runner.Runner       = runner.Exec{}
	releaseSleep           func(time.Duration) = time.Sleep
	releaseNow             func() time.Time    = time.Now
	releasePollInterval                        = 10 * time.Second
	releaseRegisterWindow                      = 60 * time.Second
	releaseWaitTimeout                         = 20 * time.Minute
	releaseWorkspacePaths  func(manifest string) (root, manifestPath string)
	releaseNewDatabaseName func() string
	releaseTester          application.Tester
)

func init() {
	releaseWorkspacePaths = func(manifest string) (string, string) {
		root := findWorkspaceRoot("")
		return root, findManifestPath(root, manifest)
	}
}

func releaseUI() application.UI {
	return application.UI{
		Arrow: iconArrow,
		OK:    iconOK,
		Warn:  iconWarn,
		Title: func(text string) string { return titleStyle.Render(text) },
		Bold:  func(text string) string { return boldStyle.Render(text) },
	}
}

func newReleaseService(out io.Writer, manifest string) *application.Service {
	root, manifestPath := releaseWorkspacePaths(manifest)
	svc := release.NewService(root, manifestPath, releaseRunner, out, releaseUI())
	svc.Sleep = releaseSleep
	svc.Now = releaseNow
	svc.PollInterval = releasePollInterval
	svc.RegisterWindow = releaseRegisterWindow
	svc.WaitTimeout = releaseWaitTimeout
	if releaseNewDatabaseName != nil {
		svc.NewDatabaseName = releaseNewDatabaseName
	}
	if releaseTester != nil {
		svc.Tester = releaseTester
	}
	return svc
}

func newReleaseCmd() *cobra.Command {
	var manifestFlag string

	cmd := &cobra.Command{
		Use:   "release",
		Short: "Inspect and promote release pull requests and demo branches",
		Long: "Watch the release-tier check of the bot release pull request, promote main to demo, " +
			"and merge the release pull request. There is no long-lived release branch: changesets " +
			"and release-please open one pull request into main per repository.",
	}
	cmd.PersistentFlags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml")

	cmd.AddCommand(newReleaseStatusCmd(&manifestFlag))
	cmd.AddCommand(newReleaseCheckCmd(&manifestFlag))
	cmd.AddCommand(newReleaseDemoCmd(&manifestFlag))
	cmd.AddCommand(newReleaseProdCmd(&manifestFlag))
	return cmd
}

func newReleaseStatusCmd(manifestFlag *string) *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "status [repo]",
		Short: "Report the release pull request, demo branch and last publish or deploy run",
		Long: "Reads the default branch SHA, latest tag, open release pull request and its checks, " +
			"the demo branch's distance from main, and the last publish or deploy workflow run. " +
			"With no repository it checks every repository that has a release setup. Exit 2 means " +
			"something could not be read.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			selector := ""
			if len(args) > 0 {
				selector = args[0]
			}
			res, err := newReleaseService(out, *manifestFlag).Status(cmd.Context(), selector)
			if jsonFlag {
				return writeReleaseResult(out, res, err)
			}
			renderReleaseStatus(out, res)
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newReleaseCheckCmd(manifestFlag *string) *cobra.Command {
	var (
		localFlag bool
		jsonFlag  bool
		waitFlag  time.Duration
	)
	cmd := &cobra.Command{
		Use:   "check [repo]",
		Short: "Watch the release-tier check, or run the release tier against the local dev services",
		Long: "Without --local, polls the open release pull request's checks until two consecutive " +
			"polls report the same complete set. Exit 0 means pass, 1 means failure, 2 means no " +
			"release pull request or no checks. With --local, creates a throwaway database in the " +
			"existing dev Postgres, runs the repository's release tier against the dev Postgres, " +
			"Redis and NATS, and drops the database in every case. It starts no containers.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			selector := ""
			if len(args) > 0 {
				selector = args[0]
			}
			res, err := newReleaseService(uiOut, *manifestFlag).Check(cmd.Context(), selector, localFlag, prdomain.MergeOptions{
				Wait:        true,
				WaitTimeout: waitFlag,
			})
			if jsonFlag {
				return writeReleaseResult(out, res, err)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&localFlag, "local", false, "Run the release tier against the existing dev Postgres, Redis and NATS")
	cmd.Flags().DurationVar(&waitFlag, "wait-timeout", releaseWaitTimeout, "Maximum time to wait for release checks")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newReleaseDemoCmd(manifestFlag *string) *cobra.Command {
	var (
		jsonFlag   bool
		override   string
		methodFlag string
	)
	cmd := &cobra.Command{
		Use:   "demo <repo>",
		Short: "Promote main to demo through a pull request",
		Long: "Opens a pull request from main to demo, creating demo from main when it is missing, " +
			"and merges it after the main commit's checks pass. The gate reads the checks of the " +
			"main commit being promoted, not the pull request's checks.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			method, err := releaseMethod(methodFlag)
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			res, err := newReleaseService(uiOut, *manifestFlag).Demo(cmd.Context(), args[0], method, prdomain.MergeOptions{
				Override:    override,
				OverrideSet: cmd.Flags().Changed("override"),
			})
			if jsonFlag {
				return writeReleaseResult(out, res, err)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&override, "override", "", "Merge despite failing checks and record the reason as a PR comment")
	cmd.Flags().StringVar(&methodFlag, "method", "merge", "Merge method (merge, squash, rebase, rebase-merge)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newReleaseProdCmd(manifestFlag *string) *cobra.Command {
	var (
		jsonFlag   bool
		override   string
		allowNo    bool
		methodFlag string
	)
	cmd := &cobra.Command{
		Use:   "prod <repo>",
		Short: "Merge the open release pull request through the merge gate",
		Long: "Merges the open release pull request through the same gate as 'orbit pr merge'. " +
			"For a Changesets library it then dispatches publish.yml; the owner still approves the " +
			"npm-publish environment and the CLI never approves it.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}
			method, err := releaseMethod(methodFlag)
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			res, err := newReleaseService(uiOut, *manifestFlag).Prod(cmd.Context(), args[0], method, prdomain.MergeOptions{
				Override:      override,
				OverrideSet:   cmd.Flags().Changed("override"),
				AllowNoChecks: allowNo,
			})
			if jsonFlag {
				return writeReleaseResult(out, res, err)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&override, "override", "", "Merge despite failing checks and record the reason as a PR comment")
	cmd.Flags().BoolVar(&allowNo, "allow-no-checks", false, "Allow merging when the pull request reports no checks")
	cmd.Flags().StringVar(&methodFlag, "method", "merge", "Merge method (merge, squash, rebase, rebase-merge)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func releaseMethod(value string) (prdomain.Method, error) {
	method, err := prdomain.ParseMethod(value)
	if err != nil {
		return "", clierr.Usage(err.Error())
	}
	return method, nil
}

// writeReleaseResult writes one JSON envelope. A failed command keeps its data
// so an unverified status still reports what was readable.
func writeReleaseResult(out io.Writer, data any, err error) error {
	if err != nil {
		envelope := output.Failure(err)
		envelope.Data = data
		if writeErr := output.Write(out, envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	return output.Write(out, output.Success(data))
}

func renderReleaseStatus(out io.Writer, result application.StatusResult) {
	fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Release Status — %d repository(ies)", len(result.Repos))))
	if len(result.Repos) == 0 {
		fmt.Fprintln(out, "No repository with a release setup was found.")
		return
	}
	for _, repo := range result.Repos {
		slug := repo.Slug
		if slug == "" {
			slug = "no GitHub remote"
		}
		fmt.Fprintf(out, "\n%s (%s)\n", boldStyle.Render(repo.Name), subtleStyle.Render(slug))
		fmt.Fprintf(out, "  branch:    %s @ %s\n", repo.DefaultBranch, shortReleaseSHA(repo.HeadSHA))
		fmt.Fprintf(out, "  latest tag: %s\n", orNone(repo.LatestTag))
		if repo.ReleasePR != nil {
			fmt.Fprintf(out, "  release PR: #%d %s -> %s (checks: %s) %s\n",
				repo.ReleasePR.Number, repo.ReleasePR.Head, repo.ReleasePR.Base,
				repo.ReleasePR.CheckState, repo.ReleasePR.URL)
		} else {
			fmt.Fprintln(out, "  release PR: none open")
		}
		if repo.Demo.Exists {
			fmt.Fprintf(out, "  demo:      exists @ %s, %d commit(s) behind %s\n",
				shortReleaseSHA(repo.Demo.SHA), repo.Demo.Behind, repo.DefaultBranch)
		} else {
			fmt.Fprintln(out, "  demo:      branch does not exist")
		}
		if repo.LastRun != nil {
			fmt.Fprintf(out, "  last run:  %s (%s, %s) %s\n",
				repo.LastRun.Name, orNone(repo.LastRun.Status), orNone(repo.LastRun.Conclusion), repo.LastRun.URL)
		} else {
			fmt.Fprintln(out, "  last run:  no publish or deploy run")
		}
		for _, message := range repo.Unverified {
			fmt.Fprintf(out, "  %s %s\n", warningStyle.Render("unverified:"), message)
		}
	}
}

func shortReleaseSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= 7 {
		return orNone(sha)
	}
	return sha[:7]
}

func orNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
