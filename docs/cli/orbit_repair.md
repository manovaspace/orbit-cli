## orbit repair

Attach .git to gitless workspace trees without overwriting files

### Synopsis

Clones each target to a temporary directory and copies .git into the existing
tree. Working files are never checkout -f or reset --hard.

Use this when orbit status reports "gitless". Repositories that already have
.git are skipped. Missing paths are cloned as with orbit init.

Scopes match orbit init / orbit status (default: all).

```
orbit repair [scope] [flags]
```

### Options

```
      --concurrency int   Number of concurrent repair workers (default 4)
  -h, --help              help for repair
      --manifest string   Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

