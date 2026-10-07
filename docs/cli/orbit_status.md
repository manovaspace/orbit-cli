## orbit status

Show git status and working tree cleanliness across workspace repositories

### Synopsis

Inspects all cloned repositories and renders a colorized table of branch names, sync states (ahead/behind), and uncommitted changes.

```
orbit status [scope] [flags]
```

### Options

```
  -h, --help              help for status
      --limit int         maximum rows per page (0 = all)
      --manifest string   Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)
      --page int          page number to display (default 1)
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
