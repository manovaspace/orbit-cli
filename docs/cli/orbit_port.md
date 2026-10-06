## orbit port

Manage and inspect the hybrid 50-port allocation model

### Synopsis

Inspect project port ranges (50-port blocks), deterministic service slots (0-9), and suggest ports (10-49) after a momentary IPv4 loopback bind probe. Suggestions are not durable reservations.

### Options

```
  -h, --help   help for port
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit port allocate](orbit_port_allocate.md)	 - Suggest a bindable dynamic port; no reservation is retained
* [orbit port list](orbit_port_list.md)	 - List base ports and deterministic slots for all registered projects
