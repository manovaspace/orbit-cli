## orbit ci secrets sync

Set every ci_secrets entry for one repo, or every repo that declares any

```
orbit ci secrets sync [repo] [flags]
```

### Options

```
  -h, --help              help for sync
      --json              Print the result as a JSON envelope
      --keys-dir string   Directory holding per-repo DEPLOY_SSH_KEY files (default "~/.config/orbit/deploy-keys")
      --source string     KEY=value file with secret values (chmod 600) (default "/home/opmc/.config/orbit/ci-secrets.env")
```

### Options inherited from parent commands

```
      --config string     Custom path to Orbit CLI configuration file
      --manifest string   Path to workspace.yaml
```

### SEE ALSO

* [orbit ci secrets](orbit_ci_secrets.md)	 - Push repository secrets declared in workspace.yaml

