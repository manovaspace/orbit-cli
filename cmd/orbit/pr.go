package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/pr/application"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
	"github.com/manovaspace/orbit-cli/pkg/pr/infrastructure/ghcli"
	"github.com/manovaspace/orbit-cli/pkg/pr/infrastructure/gitcli"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

// prRunner is the seam through which the pr commands reach git and gh.
var prRunner runner.Runner = runner.Exec{}

// Gate timings and clock are package-level so command tests run instantly.
var (
	prSleep          func(time.Duration) = time.Sleep
	prNow            func() time.Time    = time.Now
	prPollInterval                       = 10 * time.Second
	prRegisterWindow                     = 60 * time.Second
)

const defaultPRWaitTimeout = 20 * time.Minute

// prGateOptions holds the merge-gate flags.
type prGateOptions struct {
	override      string        // --override "reason"
	overrideSet   bool          // true when --override was passed (even if blank)
	allowNoChecks bool          // --allow-no-checks
	wait          bool          // true for deliver and create --merge
	waitTimeout   time.Duration // --wait-timeout, default 20m
}

func (o prGateOptions) mergeOptions() domain.MergeOptions {
	return domain.MergeOptions{
		Override:      o.override,
		OverrideSet:   o.overrideSet,
		AllowNoChecks: o.allowNoChecks,
		Wait:          o.wait,
		WaitTimeout:   o.waitTimeout,
	}
}

// addPRGateFlags registers the merge-gate flags. --wait-timeout is only offered
// on commands that wait for pending checks.
func addPRGateFlags(cmd *cobra.Command, o *prGateOptions, withWait bool) {
	cmd.Flags().StringVar(&o.override, "override", "", "Merge despite failing checks and record the reason as a PR comment")
	cmd.Flags().BoolVar(&o.allowNoChecks, "allow-no-checks", false, "Allow merging when the pull request reports no checks")
	if withWait {
		cmd.Flags().DurationVar(&o.waitTimeout, "wait-timeout", defaultPRWaitTimeout, "Maximum time to wait for pending checks")
	}
}

// newPRService wires the application layer to the real forge and git ports.
func newPRService(out io.Writer) *application.Service {
	return &application.Service{
		Forge:          ghcli.New(prRunner),
		Git:            gitcli.New(prRunner),
		Out:            out,
		Sleep:          prSleep,
		Now:            prNow,
		PollInterval:   prPollInterval,
		RegisterWindow: prRegisterWindow,
		UI: application.UI{
			Arrow: iconArrow,
			OK:    iconOK,
			Warn:  iconWarn,
			Title: func(text string) string { return titleStyle.Render(text) },
			Bold:  func(text string) string { return boldStyle.Render(text) },
		},
	}
}

func prDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "."
	}
	return dir
}

func prMethod(value string) (domain.Method, error) {
	method, err := domain.ParseMethod(value)
	if err != nil {
		return "", clierr.Usage(err.Error())
	}
	return method, nil
}

// prWriteFailure emits the JSON error envelope when --json is set.
func prWriteFailure(out io.Writer, err error) {
	_ = output.Write(out, output.Failure(err))
}

func newPRCmd() *cobra.Command {
	var dirFlag string

	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Manage pull requests and automated delivery",
		Long:  "Create, merge, inspect status, and deliver pull requests directly from your workspace repository.",
	}

	cmd.PersistentFlags().StringVar(&dirFlag, "dir", ".", "Path to repository directory (default: current directory)")

	cmd.AddCommand(newPRCreateCmd(&dirFlag))
	cmd.AddCommand(newPRMergeCmd(&dirFlag))
	cmd.AddCommand(newPRStatusCmd(&dirFlag))
	cmd.AddCommand(newPRDeliverCmd(&dirFlag))

	return cmd
}

func newPRCreateCmd(dirFlag *string) *cobra.Command {
	var (
		titleFlag  string
		bodyFlag   string
		baseFlag   string
		headFlag   string
		mergeFlag  bool
		methodFlag string
		jsonFlag   bool
		gateOpts   = prGateOptions{wait: true}
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new pull request on GitHub",
		Long:  "Pushes the head branch to origin, creates a pull request on GitHub, and optionally merges it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			method, err := prMethod(methodFlag)
			if err != nil {
				if jsonFlag {
					prWriteFailure(out, err)
				}
				return err
			}

			gateOpts.overrideSet = cmd.Flags().Changed("override")
			res, err := newPRService(uiOut).Create(cmd.Context(), prDir(*dirFlag), application.CreateInput{
				Title:  titleFlag,
				Body:   bodyFlag,
				Base:   baseFlag,
				Head:   headFlag,
				Merge:  mergeFlag,
				Method: method,
				Gate:   gateOpts.mergeOptions(),
			})
			if err != nil {
				if jsonFlag {
					prWriteFailure(out, err)
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&titleFlag, "title", "", "Pull request title (defaults to latest commit message)")
	cmd.Flags().StringVar(&bodyFlag, "body", "", "Pull request body/description")
	cmd.Flags().StringVar(&baseFlag, "base", "main", "Base branch to merge into")
	cmd.Flags().StringVar(&headFlag, "head", "", "Head branch (defaults to current branch)")
	cmd.Flags().BoolVar(&mergeFlag, "merge", false, "Merge pull request immediately after creation")
	cmd.Flags().StringVar(&methodFlag, "method", "merge", "Merge method (merge, squash, rebase, rebase-merge)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	addPRGateFlags(cmd, &gateOpts, true)

	return cmd
}

func newPRMergeCmd(dirFlag *string) *cobra.Command {
	var (
		methodFlag string
		jsonFlag   bool
		gateOpts   prGateOptions
	)

	cmd := &cobra.Command{
		Use:   "merge [number]",
		Short: "Merge a pull request on GitHub",
		Long:  "Merges an open pull request by number. If number is omitted, attempts to find and merge the open PR for the current branch.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			method, err := prMethod(methodFlag)
			if err != nil {
				if jsonFlag {
					prWriteFailure(out, err)
				}
				return err
			}

			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}

			gateOpts.overrideSet = cmd.Flags().Changed("override")
			res, err := newPRService(uiOut).Merge(cmd.Context(), prDir(*dirFlag), ref, method, gateOpts.mergeOptions())
			if err != nil {
				if jsonFlag {
					prWriteFailure(out, err)
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&methodFlag, "method", "merge", "Merge method (merge, squash, rebase, rebase-merge)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	addPRGateFlags(cmd, &gateOpts, false)

	return cmd
}

func newPRStatusCmd(dirFlag *string) *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "List open pull requests for the repository",
		Long:  "Displays all open pull requests for the current repository on GitHub.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			res, err := newPRService(out).Status(cmd.Context(), prDir(*dirFlag))
			if err != nil {
				if jsonFlag {
					prWriteFailure(out, err)
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Open Pull Requests (GitHub) — %s", res.Repo)))

			if len(res.PullRequests) == 0 {
				fmt.Fprintf(out, "No open pull requests in %s.\n", res.Repo)
				return nil
			}

			tbl := table.New(
				table.Column{Title: "#", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 6},
				table.Column{Title: "TITLE", HeaderStyle: headerStyle, MinWidth: 20, Flexible: true},
				table.Column{Title: "BRANCH", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 16},
				table.Column{Title: "AUTHOR", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 12},
				table.Column{Title: "URL", HeaderStyle: headerStyle, CellStyle: dimStyle, MinWidth: 30, Flexible: true},
			)

			for _, pullRequest := range res.PullRequests {
				branch := pullRequest.Head
				if pullRequest.Head != "" && pullRequest.Base != "" {
					branch = fmt.Sprintf("%s -> %s", pullRequest.Head, pullRequest.Base)
				}
				tbl.AddRow(fmt.Sprintf("#%d", pullRequest.Number), pullRequest.Title, branch, pullRequest.Author, pullRequest.URL)
			}

			if err := tbl.Render(out); err != nil {
				return err
			}
			fmt.Fprintf(out, "\n%s\n", infoStyle.Render(fmt.Sprintf("%d open pull request(s)", len(res.PullRequests))))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")

	return cmd
}

func newPRDeliverCmd(dirFlag *string) *cobra.Command {
	var (
		titleFlag  string
		bodyFlag   string
		baseFlag   string
		methodFlag string
		jsonFlag   bool
		gateOpts   = prGateOptions{wait: true}
	)

	cmd := &cobra.Command{
		Use:   "deliver",
		Short: "Deliver branch end-to-end: push, PR, merge, checkout base, fast-forward, and prune",
		Long:  "Executes the full Tier 1 delivery lifecycle for the active branch: pushes branch to origin, creates a PR on GitHub, merges the PR, checks out base branch, pulls --ff-only, and prunes the local feature branch.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			method, err := prMethod(methodFlag)
			if err != nil {
				if jsonFlag {
					prWriteFailure(out, err)
				}
				return err
			}

			gateOpts.overrideSet = cmd.Flags().Changed("override")
			res, err := newPRService(uiOut).Deliver(cmd.Context(), prDir(*dirFlag), application.DeliverInput{
				Title:  titleFlag,
				Body:   bodyFlag,
				Base:   baseFlag,
				Method: method,
				Gate:   gateOpts.mergeOptions(),
			})
			if err != nil {
				if jsonFlag {
					prWriteFailure(out, err)
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(res))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&titleFlag, "title", "", "Pull request title (defaults to latest commit message)")
	cmd.Flags().StringVar(&bodyFlag, "body", "", "Pull request body/description")
	cmd.Flags().StringVar(&baseFlag, "base", "main", "Base branch to deliver into")
	cmd.Flags().StringVar(&methodFlag, "method", "merge", "Merge method (merge, squash, rebase, rebase-merge)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	addPRGateFlags(cmd, &gateOpts, true)

	return cmd
}
