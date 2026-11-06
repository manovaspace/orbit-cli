## orbit pr create

Create a new pull request on GitHub

### Synopsis

Pushes the head branch to origin, creates a pull request on GitHub, and optionally merges it.

```
orbit pr create [flags]
```

### Options

```
      --allow-no-checks         Allow merging when the pull request reports no checks
      --base string             Base branch to merge into (default "main")
      --body string             Pull request body/description
      --head string             Head branch (defaults to current branch)
  -h, --help                    help for create
      --json                    Print the result as a JSON envelope
      --merge                   Merge pull request immediately after creation
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

