## orbit port

Inspect, allocate and check dev ports against the port registry

### Synopsis

Read the dev port registry (handbook/agents/workspace/ports.yaml, ADR-058) and enforce it. list shows registered services with a listener probe; allocate suggests a free port that no registry entry claims; check scans repositories for unregistered, retired, misplaced and open-bind ports. Without a registry, list shows the 50-port project blocks and allocate uses the loopback bind probe alone. Suggestions are not durable reservations.

```
orbit port [flags]
```

### Options

```
  -h, --help   help for port
      --json   Print usage errors as a JSON envelope
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit port allocate](orbit_port_allocate.md)	 - Suggest a free dynamic port that no registry entry claims; not reserved
* [orbit port check](orbit_port_check.md)	 - Check repositories against the dev port registry (ports.yaml)
* [orbit port list](orbit_port_list.md)	 - List registered services (ports.yaml) or base ports for all projects

