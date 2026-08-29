## orbit sync

Fetch and fast-forward clean default branches across workspace repositories

### Synopsis

Fetches upstream origin for all targets, verifies clean working tree, and performs fast-forward merges on default branches without overwriting uncommitted work.

```
orbit sync [scope] [flags]
```

### Options

```
      --concurrency int   Number of concurrent sync workers (default 4)
  -h, --help              help for sync
      --manifest string   Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

