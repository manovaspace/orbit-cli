## orbit doctor

Run pre-flight system diagnostics and environment health checks

### Synopsis

Local diagnostics are the default: no remote probes, updater activity or repairs. --remote selects SSH, Docker daemon, listener and cloud probes. --fix explicitly selects repairs; --accept-host-keys additionally requires --remote --fix. --local rejects remote probes and repairs.

```
orbit doctor [flags]
```

### Options

```
      --accept-host-keys   Accept new SSH host keys (requires --remote --fix)
  -f, --fix                Automatically install and configure missing toolchain dependencies
  -h, --help               help for doctor
      --json               Output diagnostic report in JSON format
      --local              Explicitly require diagnostics without remote probes or writes (default behavior)
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

