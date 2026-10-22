package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/pr"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

func newPRCmd() *cobra.Command {
	var (
		dirFlag   string
		urlFlag   string
		tokenFlag string
	)

	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Manage pull requests and automated delivery via Forgejo",
		Long:  "Create, merge, inspect status, and deliver pull requests on Forgejo directly from your workspace repository.",
	}

	cmd.PersistentFlags().StringVar(&dirFlag, "dir", ".", "Path to repository directory (default: current directory)")
	cmd.PersistentFlags().StringVar(&urlFlag, "url", "", "Custom Forgejo/Gitea base URL")
	cmd.PersistentFlags().StringVar(&tokenFlag, "token", "", "Forgejo API access token")

	cmd.AddCommand(newPRCreateCmd(&dirFlag, &urlFlag, &tokenFlag))
	cmd.AddCommand(newPRMergeCmd(&dirFlag, &urlFlag, &tokenFlag))
	cmd.AddCommand(newPRStatusCmd(&dirFlag, &urlFlag, &tokenFlag))
	cmd.AddCommand(newPRDeliverCmd(&dirFlag, &urlFlag, &tokenFlag))

	return cmd
}

func resolvePRContext(dir, customURL, customToken string) (string, string, string, *pr.Client, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	owner, repo, currentBranch, err := pr.DetectRepoInfo(dir)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("detecting repository info in %s: %w", dir, err)
	}

	workspaceRoot := findWorkspaceRoot(dir)
	cfg, err := pr.ResolveCredentials(workspaceRoot)
	if err != nil {
		if customToken != "" {
			baseURL := customURL
			if baseURL == "" {
				baseURL = pr.DefaultBaseURL
			}
			cfg = &pr.Config{
				BaseURL: baseURL,
				Token:   customToken,
			}
		} else {
			return "", "", "", nil, err
		}
	} else {
		if customURL != "" {
			cfg.BaseURL = customURL
		}
		if customToken != "" {
			cfg.Token = customToken
		}
	}

	client, err := pr.NewClient(cfg)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("initializing PR client: %w", err)
	}

	return owner, repo, currentBranch, client, nil
}

func runGit(dir string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("git %s failed: %s (%w)", strings.Join(args, " "), trimmed, err)
	}
	return trimmed, nil
}

func newPRCreateCmd(dirFlag, urlFlag, tokenFlag *string) *cobra.Command {
	var (
		titleFlag  string
		bodyFlag   string
		baseFlag   string
		headFlag   string
		mergeFlag  bool
		methodFlag string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new pull request on Forgejo",
		Long:  "Pushes the head branch to origin, creates a pull request on Forgejo, and optionally merges it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			dir := *dirFlag
			if dir == "" {
				dir = "."
			}

			owner, repo, currentBranch, client, err := resolvePRContext(dir, *urlFlag, *tokenFlag)
			if err != nil {
				return err
			}

			head := headFlag
			if strings.TrimSpace(head) == "" {
				head = currentBranch
			}
			if head == "" || head == "HEAD" {
				return errors.New("cannot determine head branch; specify --head or checkout a branch")
			}

			base := baseFlag
			if strings.TrimSpace(base) == "" {
				base = "main"
			}

			if head == base {
				return fmt.Errorf("head branch %q cannot be the same as base branch %q", head, base)
			}

			title := titleFlag
			if strings.TrimSpace(title) == "" {
				commitSubject, err := runGit(dir, "log", "-1", "--pretty=%s", head)
				if err == nil && strings.TrimSpace(commitSubject) != "" {
					title = strings.TrimSpace(commitSubject)
				}
			}
			if strings.TrimSpace(title) == "" {
				return errors.New("PR title is required (specify with --title)")
			}

			body := bodyFlag
			if strings.TrimSpace(body) == "" {
				commitBody, err := runGit(dir, "log", "-1", "--pretty=%b", head)
				if err == nil {
					body = strings.TrimSpace(commitBody)
				}
			}

			// 1. Push branch to origin
			fmt.Fprintf(out, "%s Pushing branch %s to origin...\n", iconArrow, boldStyle.Render(head))
			if _, err := runGit(dir, "push", "-u", "origin", head); err != nil {
				return fmt.Errorf("failed to push branch %q to origin: %w", head, err)
			}

			// 2. Create pull request
			fmt.Fprintf(out, "%s Creating pull request %s -> %s in %s/%s...\n", iconArrow, boldStyle.Render(head), boldStyle.Render(base), owner, repo)
			pullReq, err := client.CreatePR(owner, repo, title, body, head, base)
			if err != nil {
				return fmt.Errorf("failed to create pull request: %w", err)
			}

			fmt.Fprintf(out, "%s Pull request created: %s\n", iconOK, boldStyle.Render(pullReq.HTMLURL))

			// 3. Optional merge
			if mergeFlag {
				method := methodFlag
				if strings.TrimSpace(method) == "" {
					method = "merge"
				}
				fmt.Fprintf(out, "%s Merging pull request #%d (%s)...\n", iconArrow, pullReq.Index, method)
				if err := client.MergePR(owner, repo, pullReq.Index, method); err != nil {
					return fmt.Errorf("failed to merge pull request #%d: %w", pullReq.Index, err)
				}
				fmt.Fprintf(out, "%s Pull request #%d merged into %s (%s)\n", iconOK, pullReq.Index, boldStyle.Render(base), method)
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

	return cmd
}

func newPRMergeCmd(dirFlag, urlFlag, tokenFlag *string) *cobra.Command {
	var methodFlag string

	cmd := &cobra.Command{
		Use:   "merge [number]",
		Short: "Merge a pull request on Forgejo",
		Long:  "Merges an open pull request by number. If number is omitted, attempts to find and merge the open PR for the current branch.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			dir := *dirFlag
			if dir == "" {
				dir = "."
			}

			owner, repo, currentBranch, client, err := resolvePRContext(dir, *urlFlag, *tokenFlag)
			if err != nil {
				return err
			}

			method := methodFlag
			if strings.TrimSpace(method) == "" {
				method = "merge"
			}

			var prIndex int64
			if len(args) > 0 {
				parsed, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil || parsed <= 0 {
					return fmt.Errorf("invalid PR number %q: must be a positive integer", args[0])
				}
				prIndex = parsed
			} else {
				if currentBranch == "" || currentBranch == "HEAD" {
					return errors.New("cannot determine current branch; provide PR number explicitly: orbit pr merge <number>")
				}
				prs, err := client.ListOpenPRs(owner, repo)
				if err != nil {
					return fmt.Errorf("failed to list open pull requests: %w", err)
				}
				var matched *pr.PR
				for _, p := range prs {
					if p.Head != nil && p.Head.Name == currentBranch {
						matched = p
						break
					}
				}
				if matched == nil {
					return fmt.Errorf("no open pull request found for branch %q in %s/%s", currentBranch, owner, repo)
				}
				prIndex = matched.Index
			}

			fmt.Fprintf(out, "%s Merging pull request #%d in %s/%s (%s)...\n", iconArrow, prIndex, owner, repo, method)
			if err := client.MergePR(owner, repo, prIndex, method); err != nil {
				return fmt.Errorf("failed to merge pull request #%d: %w", prIndex, err)
			}

			fmt.Fprintf(out, "%s Pull request #%d merged using method %q\n", iconOK, prIndex, method)
			return nil
		},
	}

	cmd.Flags().StringVar(&methodFlag, "method", "merge", "Merge method (merge, squash, rebase, rebase-merge)")

	return cmd
}

func newPRStatusCmd(dirFlag, urlFlag, tokenFlag *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "List open pull requests for the repository",
		Long:  "Displays all open pull requests for the current repository on Forgejo.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			dir := *dirFlag
			if dir == "" {
				dir = "."
			}

			owner, repo, _, client, err := resolvePRContext(dir, *urlFlag, *tokenFlag)
			if err != nil {
				return err
			}

			prs, err := client.ListOpenPRs(owner, repo)
			if err != nil {
				return fmt.Errorf("failed to list open pull requests: %w", err)
			}

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Open Pull Requests — %s/%s", owner, repo)))

			if len(prs) == 0 {
				fmt.Fprintf(out, "No open pull requests in %s/%s.\n", owner, repo)
				return nil
			}

			tbl := table.New(
				table.Column{Title: "#", HeaderStyle: headerStyle, CellStyle: boldStyle, MinWidth: 6},
				table.Column{Title: "TITLE", HeaderStyle: headerStyle, MinWidth: 20, Flexible: true},
				table.Column{Title: "BRANCH", HeaderStyle: headerStyle, CellStyle: subtleStyle, MinWidth: 16},
				table.Column{Title: "AUTHOR", HeaderStyle: headerStyle, MinWidth: 12},
				table.Column{Title: "URL", HeaderStyle: headerStyle, CellStyle: dimStyle, MinWidth: 30, Flexible: true},
			)

			for _, p := range prs {
				author := ""
				if p.Poster != nil {
					author = p.Poster.UserName
				}
				branch := ""
				if p.Head != nil && p.Base != nil {
					branch = fmt.Sprintf("%s -> %s", p.Head.Name, p.Base.Name)
				} else if p.Head != nil {
					branch = p.Head.Name
				}
				tbl.AddRow(
					fmt.Sprintf("#%d", p.Index),
					p.Title,
					branch,
					author,
					p.HTMLURL,
				)
			}

			if err := tbl.Render(out); err != nil {
				return err
			}
			fmt.Fprintf(out, "\n%s\n", infoStyle.Render(fmt.Sprintf("%d open pull request(s)", len(prs))))
			return nil
		},
	}

	return cmd
}

func newPRDeliverCmd(dirFlag, urlFlag, tokenFlag *string) *cobra.Command {
	var (
		titleFlag  string
		bodyFlag   string
		baseFlag   string
		methodFlag string
	)

	cmd := &cobra.Command{
		Use:   "deliver",
		Short: "Deliver branch end-to-end: push, PR, merge, checkout base, fast-forward, and prune",
		Long:  "Executes the full Tier 1 delivery lifecycle for the active branch: pushes branch to origin, creates a Forgejo PR, merges the PR, checks out base branch, pulls --ff-only, and prunes the local feature branch.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			dir := *dirFlag
			if dir == "" {
				dir = "."
			}

			owner, repo, currentBranch, client, err := resolvePRContext(dir, *urlFlag, *tokenFlag)
			if err != nil {
				return err
			}

			base := baseFlag
			if strings.TrimSpace(base) == "" {
				base = "main"
			}

			if currentBranch == base {
				return fmt.Errorf("cannot deliver from %q branch: deliver must be run from a feature branch", base)
			}
			if currentBranch == "" || currentBranch == "HEAD" {
				return errors.New("cannot deliver from detached HEAD state; checkout a feature branch first")
			}

			// Verify working tree cleanliness
			statusOut, err := runGit(dir, "status", "--porcelain")
			if err != nil {
				return fmt.Errorf("checking git status: %w", err)
			}
			if strings.TrimSpace(statusOut) != "" {
				return errors.New("working tree has uncommitted changes; please commit or stash them before delivering")
			}

			title := titleFlag
			if strings.TrimSpace(title) == "" {
				commitSubject, err := runGit(dir, "log", "-1", "--pretty=%s", currentBranch)
				if err == nil && strings.TrimSpace(commitSubject) != "" {
					title = strings.TrimSpace(commitSubject)
				}
			}
			if strings.TrimSpace(title) == "" {
				return errors.New("PR title is required (specify with --title)")
			}

			body := bodyFlag
			if strings.TrimSpace(body) == "" {
				commitBody, err := runGit(dir, "log", "-1", "--pretty=%b", currentBranch)
				if err == nil {
					body = strings.TrimSpace(commitBody)
				}
			}

			method := methodFlag
			if strings.TrimSpace(method) == "" {
				method = "merge"
			}

			fmt.Fprintln(out, titleStyle.Render(fmt.Sprintf("Orbit Deliver — %s/%s (%s -> %s)", owner, repo, currentBranch, base)))

			// 1. Push branch to origin
			fmt.Fprintf(out, "%s Pushing branch %s to origin...\n", iconArrow, boldStyle.Render(currentBranch))
			if _, err := runGit(dir, "push", "-u", "origin", currentBranch); err != nil {
				return fmt.Errorf("failed to push branch %q to origin: %w", currentBranch, err)
			}
			fmt.Fprintf(out, "  %s Pushed branch %s to origin\n", iconOK, boldStyle.Render(currentBranch))

			// 2. Create PR
			fmt.Fprintf(out, "%s Creating pull request %s -> %s...\n", iconArrow, boldStyle.Render(currentBranch), boldStyle.Render(base))
			pullReq, err := client.CreatePR(owner, repo, title, body, currentBranch, base)
			if err != nil {
				return fmt.Errorf("failed to create pull request: %w", err)
			}
			fmt.Fprintf(out, "  %s Created PR #%d: %s\n", iconOK, pullReq.Index, boldStyle.Render(pullReq.HTMLURL))

			// 3. Merge PR
			fmt.Fprintf(out, "%s Merging pull request #%d (%s)...\n", iconArrow, pullReq.Index, method)
			if err := client.MergePR(owner, repo, pullReq.Index, method); err != nil {
				return fmt.Errorf("failed to merge pull request #%d: %w", pullReq.Index, err)
			}
			fmt.Fprintf(out, "  %s Merged PR #%d into %s\n", iconOK, pullReq.Index, boldStyle.Render(base))

			// 4. Switch back to base
			fmt.Fprintf(out, "%s Checking out %s...\n", iconArrow, boldStyle.Render(base))
			if _, err := runGit(dir, "checkout", base); err != nil {
				return fmt.Errorf("failed to checkout %s: %w", base, err)
			}
			fmt.Fprintf(out, "  %s Switched to branch %s\n", iconOK, boldStyle.Render(base))

			// 5. Fast-forward pull
			fmt.Fprintf(out, "%s Fast-forwarding %s to origin/%s...\n", iconArrow, boldStyle.Render(base), base)
			if _, err := runGit(dir, "pull", "--ff-only"); err != nil {
				return fmt.Errorf("failed to pull --ff-only on %s: %w", base, err)
			}
			fmt.Fprintf(out, "  %s Fast-forwarded %s to origin/%s\n", iconOK, boldStyle.Render(base), base)

			// 6. Prune local branch
			fmt.Fprintf(out, "%s Pruning local branch %s...\n", iconArrow, boldStyle.Render(currentBranch))
			delOut, err := runGit(dir, "branch", "-d", currentBranch)
			if err != nil {
				if strings.Contains(delOut, "not fully merged") {
					if _, errForce := runGit(dir, "branch", "-D", currentBranch); errForce != nil {
						return fmt.Errorf("failed to force-prune local branch %q: %w", currentBranch, errForce)
					}
				} else {
					return fmt.Errorf("failed to prune local branch %q: %w", currentBranch, err)
				}
			}
			fmt.Fprintf(out, "  %s Pruned local branch %s\n", iconOK, boldStyle.Render(currentBranch))

			// 7. Success message
			fmt.Fprintf(out, "\n%s Successfully delivered %s into %s!\n", iconOK, boldStyle.Render(currentBranch), boldStyle.Render(base))
			return nil
		},
	}

	cmd.Flags().StringVar(&titleFlag, "title", "", "Pull request title (defaults to latest commit message)")
	cmd.Flags().StringVar(&bodyFlag, "body", "", "Pull request body/description")
	cmd.Flags().StringVar(&baseFlag, "base", "main", "Base branch to deliver into")
	cmd.Flags().StringVar(&methodFlag, "method", "merge", "Merge method (merge, squash, rebase, rebase-merge)")

	return cmd
}
