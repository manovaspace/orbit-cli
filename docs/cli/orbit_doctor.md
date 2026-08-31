## orbit doctor

Run pre-flight system diagnostics and environment health checks

### Synopsis

Executes comprehensive diagnostics across OS, Go compiler, Node/Bun, Docker, SSH keys, dev ports, and optional tools.

```
orbit doctor [flags]
```

### Options

```
  -f, --fix               Automatically install and configure missing toolchain dependencies
  -h, --help              help for doctor
      --json              Output diagnostic report in JSON format
      --non-interactive   Disable interactive prompts
  -y, --yes               Skip interactive confirmation prompts
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

