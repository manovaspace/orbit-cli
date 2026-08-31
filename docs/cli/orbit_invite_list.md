## orbit invite list

List active and stored developer onboarding invitations

### Synopsis

Displays a table or JSON array of stored invitations with their status, scope, and expiration. By default, only active invitations are shown; use --all to include revoked and expired.

```
orbit invite list [flags]
```

### Options

```
  -a, --all                 Include revoked and expired invitations
  -f, --format string       Output format (table or json) (default "table")
  -h, --help                help for list
      --store-file string   Custom path to invites storage file
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit invite](orbit_invite.md)	 - Manage developer onboarding invite tokens

