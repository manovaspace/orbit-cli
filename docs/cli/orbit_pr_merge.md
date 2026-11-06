## orbit pr merge

Merge a pull request on GitHub

### Synopsis

Merges an open pull request by number. If number is omitted, attempts to find and merge the open PR for the current branch.

```
orbit pr merge [number] [flags]
```

### Options

```
      --allow-no-checks   Allow merging when the pull request reports no checks
  -h, --help              help for merge
      --json              Print the result as a JSON envelope
      --method string     Merge method (merge, squash, rebase, rebase-merge) (default "merge")
      --override string   Merge despite failing checks and record the reason as a PR comment
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
      --dir string      Path to repository directory (default: current directory) (default ".")
```

### SEE ALSO

* [orbit pr](orbit_pr.md)	 - Manage pull requests and automated delivery

