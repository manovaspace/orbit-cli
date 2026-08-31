## orbit uninstall

Uninstall Orbit CLI binaries and cleanup shell configuration

### Synopsis

Removes Orbit CLI binaries from system and user paths, cleans shell completions, and optionally purges cache, session, and the owner vault.

```
orbit uninstall [flags]
```

### Options

```
      --force             Alias for --yes
  -h, --help              help for uninstall
      --purge-state       Purge cache, session, and owner vault (~/.orbit, ~/.manova, ~/.config/orbit, ~/.config/manova)
      --purge-workspace   Purge workspace repositories (blocked if uncommitted changes exist)
  -y, --yes               Uninstall without confirmation prompt
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

