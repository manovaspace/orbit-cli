package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/pr/application"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
	"github.com/manovaspace/orbit-cli/pkg/pr/infrastructure/ghcli"
	"github.com/manovaspace/orbit-cli/pkg/pr/infrastructure/gitcli"
	"github.com/manovaspace/orbit-cli/pkg/render"
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
	var (
		jsonFlag     bool
		manifestFlag string
		noURLFlag    bool
	)

	cmd := &cobra.Command{
		Use:   "status [pattern]",
		Short: "List open pull requests for one repository or matching workspace repositories",
		Long: "Displays all open pull requests for the current repository on GitHub.\n\n" +
			"With a pattern, selects workspace repositories by name instead. Only '*' is special and matches any run of characters; everything else is literal and matching is case-insensitive. Quote the pattern in the shell so it is not glob-expanded.",
		Example: "  orbit pr status\n" +
			"  orbit pr status fryto-ops\n" +
			"  orbit pr status 'fryto*'\n" +
			"  orbit pr status '*'",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			pattern := ""
			if len(args) > 0 {
				pattern = args[0]
			}
			if pattern != "" {
				if cmd.Flags().Changed("dir") {
					err := clierr.Usage("--dir cannot be combined with a repository pattern")
					if jsonFlag {
						prWriteFailure(out, err)
					}
					return err
				}
				workspaceRoot := findWorkspaceRoot("")
				manifestPath := findManifestPath(workspaceRoot, manifestFlag)
				return runWorkspacePRStatus(cmd.Context(), out, workspaceRoot, manifestPath, pattern, jsonFlag, !noURLFlag)
			}

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

			results := []prStatusRepoResult{{Repo: res.Repo.Name, PullRequests: res.PullRequests}}
			return renderPRStatusText(out, results, len(res.PullRequests), !noURLFlag)
		},
	}

	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	cmd.Flags().BoolVar(&noURLFlag, "no-url", false, "Hide pull request URLs")

	return cmd
}

// prStatusConcurrency bounds how many repositories are queried at once.
const prStatusConcurrency = 4

// prStatusRepoResult is one repository's outcome in a pattern run. Err carries
// the original error for exit-code mapping and is not serialized.
type prStatusRepoResult struct {
	Repo         string               `json:"repo"`
	PullRequests []domain.PullRequest `json:"pullRequests"`
	Skipped      string               `json:"skipped"`
	Error        string               `json:"error"`
	Err          error                `json:"-"`
}

// prStatusWorkspaceResult is the --json data shape of a pattern run.
type prStatusWorkspaceResult struct {
	Repos []prStatusRepoResult `json:"repos"`
	Total int                  `json:"total"`
}

// runWorkspacePRStatus lists open pull requests for every manifest repository
// whose name matches pattern.
func runWorkspacePRStatus(ctx context.Context, out io.Writer, workspaceRoot, manifestPath, pattern string, jsonFlag, showURL bool) error {
	manifestFile, err := manifest.Load(manifestPath)
	if err != nil {
		failure := &clierr.Error{
			Code:    clierr.CodeFailed,
			Exit:    clierr.ExitFailed,
			Message: fmt.Sprintf("failed to load workspace manifest %s", manifestPath),
			Err:     err,
		}
		if jsonFlag {
			prWriteFailure(out, failure)
		}
		return failure
	}

	targets := manifestFile.MatchRepos(pattern)
	if len(targets) == 0 && pattern != "*" {
		failure := clierr.Usage(fmt.Sprintf("no repositories match %q", pattern))
		if jsonFlag {
			prWriteFailure(out, failure)
		}
		return failure
	}

	results := statusAcrossRepos(ctx, workspaceRoot, targets)
	data := prStatusWorkspaceResult{Repos: results, Total: 0}
	for _, result := range results {
		data.Total += len(result.PullRequests)
	}
	failure := firstRepoFailure(results)

	if jsonFlag {
		envelope := output.Success(data)
		if failure != nil {
			envelope = output.Failure(failure)
			envelope.Data = data
		}
		if err := output.Write(out, envelope); err != nil {
			return err
		}
		return failure
	}

	if err := renderPRStatusText(out, results, data.Total, showURL); err != nil {
		return err
	}
	return failure
}

// statusAcrossRepos queries the repositories with bounded concurrency and
// preserves their manifest order.
func statusAcrossRepos(ctx context.Context, workspaceRoot string, targets []manifest.RepoTarget) []prStatusRepoResult {
	results := make([]prStatusRepoResult, len(targets))
	if len(targets) == 0 {
		return results
	}

	concurrency := prStatusConcurrency
	if len(targets) < concurrency {
		concurrency = len(targets)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = inspectRepoPRs(ctx, workspaceRoot, targets[index])
			}
		}()
	}
	for index := range targets {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

// inspectRepoPRs queries one repository, reporting absent and gitless trees as
// skipped rather than as failures.
func inspectRepoPRs(ctx context.Context, workspaceRoot string, target manifest.RepoTarget) prStatusRepoResult {
	result := prStatusRepoResult{Repo: target.Name, PullRequests: []domain.PullRequest{}}
	dir := filepath.Join(workspaceRoot, target.Path)

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		result.Skipped = "not cloned"
		return result
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		result.Skipped = "gitless — run orbit repair"
		return result
	}

	status, err := newPRService(io.Discard).Status(ctx, dir)
	if err != nil {
		result.Error = err.Error()
		result.Err = err
		return result
	}
	if status.PullRequests != nil {
		result.PullRequests = status.PullRequests
	}
	return result
}

// firstRepoFailure returns the highest-severity repository error, keeping the
// first one on a tie. The returned error keeps the original exit code and names
// the failing repository.
func firstRepoFailure(results []prStatusRepoResult) error {
	selected := -1
	for index, result := range results {
		if result.Err == nil {
			continue
		}
		if selected >= 0 && clierr.ExitCode(result.Err) <= clierr.ExitCode(results[selected].Err) {
			continue
		}
		selected = index
	}
	if selected < 0 {
		return nil
	}
	result := results[selected]
	return &clierr.Error{
		Code:    clierr.Code(result.Err),
		Exit:    clierr.ExitCode(result.Err),
		Message: fmt.Sprintf("pr status failed for %s", result.Repo),
		Err:     result.Err,
	}
}

// prStatusRow is one pull request in the flat status table.
type prStatusRow struct {
	Repo        string
	PullRequest domain.PullRequest
}

// renderPRStatusText prints one flat table for every open pull request, with
// each URL on the line below its row, then collapses empty, skipped and failed
// repositories into summary lines.
func renderPRStatusText(out io.Writer, results []prStatusRepoResult, total int, showURL bool) error {
	var (
		noPRs   []string
		skipped []string
		failed  []string
		rows    []prStatusRow
		queried int
	)

	for _, result := range results {
		switch {
		case result.Skipped != "":
			skipped = append(skipped, fmt.Sprintf("%s (%s)", result.Repo, result.Skipped))
			continue
		case result.Err != nil:
			failed = append(failed, fmt.Sprintf("%s (%s)", result.Repo, result.Error))
			continue
		}

		queried++
		if len(result.PullRequests) == 0 {
			noPRs = append(noPRs, result.Repo)
			continue
		}
		for _, pullRequest := range result.PullRequests {
			rows = append(rows, prStatusRow{Repo: result.Repo, PullRequest: pullRequest})
		}
	}

	fmt.Fprintln(out, render.Title.Render("Open pull requests"))

	if len(rows) > 0 {
		fmt.Fprintln(out)
		if err := renderPRTable(out, rows, showURL); err != nil {
			return err
		}
	}

	if len(noPRs)+len(skipped)+len(failed) > 0 {
		fmt.Fprintln(out)
	}
	if line := joinRepoNames(skipped, "Skipped: "); line != "" {
		fmt.Fprintln(out, warningStyle.Render(line))
	}
	if line := joinRepoNames(failed, "Failed: "); line != "" {
		fmt.Fprintln(out, errorStyle.Render(line))
	}
	if len(noPRs) > 0 {
		line := fmt.Sprintf("No open PRs in %d %s", len(noPRs), render.Plural(len(noPRs), "repository", "repositories"))
		fmt.Fprintln(out, subtleStyle.Render(line))
	}

	summary := fmt.Sprintf("%d open %s", total, render.Plural(total, "pull request", "pull requests"))
	if len(results) > 1 {
		summary += fmt.Sprintf(" across %d %s", queried, render.Plural(queried, "repository", "repositories"))
	}
	fmt.Fprintf(out, "\n%s\n", infoStyle.Render(summary))
	return nil
}

// joinRepoNames renders one collapsed summary line, or "" when the list is
// empty.
func joinRepoNames(names []string, prefix string) string {
	if len(names) == 0 {
		return ""
	}
	return prefix + strings.Join(names, ", ")
}

// renderPRTable renders the shared open-pull-request table.
func renderPRTable(out io.Writer, rows []prStatusRow, showURL bool) error {
	tbl := table.New(
		table.Column{Title: "REPO", HeaderStyle: headerStyle, MinWidth: 8},
		table.Column{Title: "PR", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 4},
		table.Column{Title: "TITLE", HeaderStyle: headerStyle, Flexible: true, EndTruncate: true, MinWidth: 20, MaxWidth: 72},
		table.Column{Title: "BRANCH", HeaderStyle: headerStyle, CellStyle: subtleStyle, Flexible: true, MinWidth: 12, MaxWidth: 48},
		table.Column{Title: "AUTHOR", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 8},
	).WithDetailColumn(1).WithDetailStyle(dimStyle)

	for _, row := range rows {
		values := []string{
			row.Repo,
			fmt.Sprintf("#%d", row.PullRequest.Number),
			row.PullRequest.Title,
			row.PullRequest.Head,
			row.PullRequest.Author,
		}
		if showURL && row.PullRequest.URL != "" {
			tbl.AddDetailRow(row.PullRequest.URL, values...)
			continue
		}
		tbl.AddRow(values...)
	}

	return render.NewPrinter(out).Table(tbl)
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
