## orbit onboard

Interactive onboarding wizard with resume, claims, and stack provisioning

### Synopsis

Interactive onboarding wizard that sets up developer identity, SSH keys,
repository clones, Cursor MCP integrations, and local development infrastructure.

Pro capabilities:
  --resume                          Resume an interrupted onboarding session
  --ignore-and-remove-checkpoint    Discard saved incomplete session checkpoint and start fresh
  --rollback                        Revert cloned repositories and provisioned credentials
  --diag-bundle                     Generate a sanitized diagnostic bundle for troubleshooting
  --auto-fix                        Automatically remediate missing prerequisites and toolchains
  --dry-run                         Run pre-flight diagnostics and preview onboarding actions

```
orbit onboard [flags]
```

### Options

```
  -f, --auto-fix                         Automatically install missing prerequisites and toolchain dependencies
      --diag-bundle string[="default"]   Generate a sanitized diagnostic tar.gz bundle (optional filename)
      --dry-run                          Run pre-flight check and preview onboarding actions without making changes
      --edge-url string                  Onboarding edge gateway URL (default: $ORBIT_SERVER or https://orbit.manova.space)
      --email string                     Developer email address
  -h, --help                             help for onboard
      --ignore-and-remove-checkpoint     Discard saved incomplete session checkpoint and start fresh
      --json                             Output JSON progress event stream
      --manifest string                  Path to workspace.yaml manifest
  -n, --name string                      Developer display name
      --non-interactive                  Run in non-interactive mode without prompting
      --resume                           Resume interrupted onboarding session from saved checkpoint
      --rollback                         Rollback provisioned resources and clear session
  -s, --server string                    Orbit server URL (alias for --edge-url)
      --session-file string              Custom path to session persistence file
      --skip-stack                       Skip local dev stack initialization
      --ssh-dir string                   Custom SSH directory path (default: ~/.ssh)
      --start-stack                      Automatically start local dev stack without prompting
  -t, --token string                     Cryptographically signed onboarding invite token
  -u, --uid string                       Desired username / UID
      --workspace string                 Target workspace root directory
  -y, --yes                              Skip interactive confirmation prompts and automatically proceed
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

