## orbit release demo

Promote main to demo through a pull request

### Synopsis

Opens a pull request from main to demo, creating demo from main when it is missing, and merges it after the main commit's checks pass. The gate reads the checks of the main commit being promoted, not the pull request's checks.

```
orbit release demo <repo> [flags]
```

### Options

```
  -h, --help              help for demo
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

