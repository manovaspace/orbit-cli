## orbit services

Show the state of the local services

### Synopsis

Show whether Postgres and Mailpit run, on which port, and whether orbit or another process owns the listener. Reads only; nothing is started or stopped.

```
orbit services [flags]
```

### Options

```
  -h, --help   help for services
      --json   Print the result as a JSON envelope
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

