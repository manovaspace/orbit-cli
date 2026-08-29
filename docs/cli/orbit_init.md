## orbit init

Clone and initialize workspace repositories

### Synopsis

Resolves repository targets from workspace.yaml and clones them concurrently.
Scopes:
  core            - Clones essential core baseline (default)
  all / *         - Clones all repositories in the manifest
  orbit           - Clones Orbit platform toolkit
  manovaspace     - Clones open-source commons
  clients/<name>  - Clones a specific client cluster
  <repo-name>     - Clones an individual repository

```
orbit init [scope] [flags]
```

### Options

```
      --bootstrap         Bootstrap workspace without failing if workspace.yaml is not yet cloned
      --concurrency int   Number of concurrent clone workers (default 4)
  -h, --help              help for init
      --manifest string   Path to workspace.yaml (default: <workspaceRoot>/workspace.yaml)
      --skip-hooks        Skip post-clone workspace hooks and migrations
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

