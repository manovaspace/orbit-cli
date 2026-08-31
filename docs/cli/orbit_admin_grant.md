## orbit admin grant

Generate an 8-digit single-use authorization code for a new admin

### Synopsis

Generates a single-use 8-digit administrative grant code (e.g. 8492-0194) bound to the
specified email address. The new admin uses this code with 'orbit admin init <email> --code 8492-0194'
to initialize their workstation without sending or receiving public challenge emails.

```
orbit admin grant <email> [flags]
```

### Options

```
      --code string     Explicit 8-digit code (auto-generated if omitted)
  -h, --help            help for grant
      --json            Output grant details as JSON
      --role string     Role to grant (admin, maintainer, superadmin) (default "admin")
      --send            Dispatch grant code directly to recipient via SMTP
  -s, --server string   Orbit server URL
      --store string    Custom path to owner storage vault file
      --telegram        Dispatch grant code to Telegram Secrets topic
      --ttl duration    Grant validity duration (e.g. 15m, 1h) (default 15m0s)
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit admin](orbit_admin.md)	 - Manage platform ownership verification and root cryptographic secrets

