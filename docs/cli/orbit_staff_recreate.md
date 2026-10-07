## orbit staff recreate

Delete then create a staff member (fresh SSO + mailbox)

### Synopsis

Wipes Authelia TOTP, the Stalwart mailbox, and the lldap user, then creates them again. Reserved directory accounts (admin, authelia-bind, verdaccio-bind, verdaccio-ci) are rejected.

```
orbit staff recreate [flags]
```

### Options

```
      --forward string           personal forward email (required)
      --groups string            comma-separated groups (default server-side: dev)
  -h, --help                     help for recreate
      --idempotency-key string   idempotency key for create (generated if empty)
      --invite                   generate and print a signed onboarding invite token after account recreation
      --invite-email string      email address for the invite token (defaults to --forward value)
      --invite-ttl string        invite token TTL (e.g. 7d, 24h, 168h) (default "7d")
      --name string              display name
      --no-send                  suppress dispatching onboarding invitation email
      --totp                     enroll Authelia TOTP after recreate
      --uid string               staff uid (required)
```

### Options inherited from parent commands

```
      --config string        Custom path to Orbit CLI configuration file
      --owner-store string   path to owner.json vault
      --server string        staff server URL (or ORBIT_STAFF_URL)
```

### SEE ALSO

* [orbit staff](orbit_staff.md)	 - Manage Orbit staff via the staff control plane
