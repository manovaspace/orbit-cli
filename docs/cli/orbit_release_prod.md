## orbit release prod

Merge the open release pull request through the merge gate

### Synopsis

Merges the open release pull request through the same gate as 'orbit pr merge'. For a Changesets library it then dispatches publish.yml; the owner still approves the npm-publish environment and the CLI never approves it.

```
orbit release prod <repo> [flags]
```

### Options

```
      --allow-no-checks   Allow merging when the pull request reports no checks
  -h, --help              help for prod
      --json              Print the result as a JSON envelope
      --method string     Merge method (merge, squash, rebase, rebase-merge) (default "merge")
      --override string   Merge despite failing checks and record the reason as a PR comment
```

### Options inherited from parent commands

```
      --config string     Custom path to Orbit CLI configuration file
      --manifest string   Path to workspace.yaml
```

### SEE ALSO

* [orbit release](orbit_release.md)	 - Inspect and promote release pull requests and demo branches

