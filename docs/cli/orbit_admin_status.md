## orbit admin status

Display platform ownership verification status, vault integrity, and mail config

### Synopsis

Reports whether platform ownership has been verified, vault file path and permissions, and active SMTP gateway.

```
orbit admin status [flags]
```

### Options

```
  -f, --format string   Output format: table or json (default "table")
  -h, --help            help for status
      --store string    Custom path to owner storage vault file
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit admin](orbit_admin.md)	 - Manage platform ownership verification and root cryptographic secrets

