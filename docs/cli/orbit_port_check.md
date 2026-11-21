## orbit port check

Check repositories against the dev port registry (ports.yaml)

### Synopsis

Scan compose files, package.json scripts, .env.example files, Go sources, Caddy files and TS/JS sources for ports 10000-17999 and report numbers the registry does not list (UNREGISTERED), retired numbers (RETIRED), binds in a repository the entry does not list (WRONG_REPO), binds of reserved slots (RESERVED) and compose publishes on all interfaces (OPEN_BIND, a warning unless --strict). Exit codes: 0 no findings, 1 at least one finding, 2 the registry is missing or a file could not be read.

```
orbit port check [flags]
```

### Options

```
  -h, --help          help for check
      --json          Print the result as a JSON envelope
      --root string   workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)
      --strict        Treat OPEN_BIND warnings as failures
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit port](orbit_port.md)	 - Inspect, allocate and check dev ports against the port registry

