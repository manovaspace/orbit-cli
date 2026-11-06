## orbit pr deliver

Deliver branch end-to-end: push, PR, merge, checkout base, fast-forward, and prune

### Synopsis

Executes the full Tier 1 delivery lifecycle for the active branch: pushes branch to origin, creates a PR on GitHub, merges the PR, checks out base branch, pulls --ff-only, and prunes the local feature branch.

```
orbit pr deliver [flags]
```

### Options

```
      --allow-no-checks         Allow merging when the pull request reports no checks
      --base string             Base branch to deliver into (default "main")
      --body string             Pull request body/description
  -h, --help                    help for deliver
      --json                    Print the result as a JSON envelope
      --method string           Merge method (merge, squash, rebase, rebase-merge) (default "merge")
      --override string         Merge despite failing checks and record the reason as a PR comment
      --title string            Pull request title (defaults to latest commit message)
      --wait-timeout duration   Maximum time to wait for pending checks (default 20m0s)
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
      --dir string      Path to repository directory (default: current directory) (default ".")
```

### SEE ALSO

* [orbit pr](orbit_pr.md)	 - Manage pull requests and automated delivery

