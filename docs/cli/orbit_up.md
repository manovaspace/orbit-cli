## orbit up

Start the local services this machine needs

### Synopsis

Start the native Postgres cluster and Mailpit that the selected repositories declare, creating the cluster and missing databases first. Services are left running; stop them with 'o down'. With services: external in machine.yaml, nothing is checked or started and the command exits 0.

```
orbit up [flags]
```

### Options

```
  -h, --help   help for up
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

