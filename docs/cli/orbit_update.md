## orbit update

Unified workspace update (CLI, git branches, migrations, and env validation)

### Synopsis

Performs a full workspace synchronization and verification:
  1. Checks for Orbit CLI updates
  2. Synchronizes all clean default git branches from origin
  3. Executes pending workspace migrations (.orbit/migrations.json)
  4. Validates project .env files against .env.schema.yaml contracts

```
orbit update [flags]
```

### Options

```
      --concurrency int   Concurrent git sync workers (default 4)
  -h, --help              help for update
      --manifest string   Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)
      --skip-env          Skip environment validation
      --skip-migrate      Skip workspace migrations
      --skip-selfupdate   Skip CLI update check
      --skip-sync         Skip workspace git sync
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

