## orbit doctor

Check this machine against the setup, without changing it

### Synopsis

Checks the machine against the setup selected in machine.yaml and lists the pending changes; it never changes the machine. A pending change or a failed check exits 1; run 'o setup' to apply the changes. --remote adds SSH, Docker daemon, listener and cloud probes. --local is the default and accepted as a no-op.

```
orbit doctor [flags]
```

### Options

```
      --accept-host-keys   Refused: doctor never changes the machine
  -f, --fix                Refused: doctor never changes the machine; run o setup
  -h, --help               help for doctor
      --json               Print the result as a JSON envelope
      --local              Accepted for compatibility; the check is local by default
      --non-interactive    Compatibility flag; doctor does not prompt
      --remote             Opt in to remote SSH/cloud and daemon/listener probes
  -y, --yes                Compatibility flag; doctor does not prompt
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

