## orbit port list

List registered services (ports.yaml) or base ports for all projects

### Synopsis

With a handbook/agents/workspace/ports.yaml registry, list every registered service by port with a live listener probe. Without one, list the 50-port project blocks. --limit and --page page projects, or services when a registry is found.

```
orbit port list [flags]
```

### Options

```
  -h, --help          help for list
      --json          Print the result as a JSON envelope
      --limit int     maximum projects (or services, with a registry) per page (0 = all)
      --page int      page number to display (default 1)
      --root string   workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)
      --scan          Momentarily attempt loopback binds for every block slot (no effect with a registry, which always probes)
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit port](orbit_port.md)	 - Inspect, allocate and check dev ports against the port registry

