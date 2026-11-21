## orbit dev routes

Generate the Caddyfile that serves native dev processes at <name>.localhost

### Synopsis

Route every registry service with runs: mac and a localhost name (ports.yaml) to http://<name>.localhost through Caddy, a reverse proxy to 127.0.0.1:<port>. The default prints the Caddyfile on stdout; --write writes it to a path, --list prints a table, and --json prints the list as a JSON envelope.

```
orbit dev routes [flags]
```

### Options

```
  -h, --help           help for routes
      --json           print the routes as a JSON envelope
      --list           print the routes as a table (NAME, URL, PORT, SERVICE) instead of the Caddyfile
      --root string    workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)
      --write string   write the Caddyfile to this path (parent directories are created) and print the path
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit dev](orbit_dev.md)	 - Control and orchestrate local development stacks and containers

