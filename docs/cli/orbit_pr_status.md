## orbit pr status

List open pull requests for one repository or matching workspace repositories

### Synopsis

Displays all open pull requests for the current repository on GitHub.

With a pattern, selects workspace repositories by name instead. Only '*' is special and matches any run of characters; everything else is literal and matching is case-insensitive. Quote the pattern in the shell so it is not glob-expanded.

```
orbit pr status [pattern] [flags]
```

### Examples

```
  orbit pr status
  orbit pr status falak
  orbit pr status 'falak*'
  orbit pr status '*'
```

### Options

```
  -h, --help              help for status
      --json              Print the result as a JSON envelope
      --manifest string   Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)
      --no-url            Hide pull request URLs
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
      --dir string      Path to repository directory (default: current directory) (default ".")
```

### SEE ALSO

* [orbit pr](orbit_pr.md)	 - Manage pull requests and automated delivery

