## orbit release status

Report the release pull request, demo branch and last publish or deploy run

### Synopsis

Reads the default branch SHA, latest tag, open release pull request and its checks, the demo branch's distance from main, and the last publish or deploy workflow run. With no repository it checks every repository that has a release setup. Exit 2 means something could not be read.

```
orbit release status [repo] [flags]
```

### Options

```
  -h, --help   help for status
      --json   Print the result as a JSON envelope
```

### Options inherited from parent commands

```
      --config string     Custom path to Orbit CLI configuration file
      --manifest string   Path to workspace.yaml
```

### SEE ALSO

* [orbit release](orbit_release.md)	 - Inspect and promote release pull requests and demo branches

