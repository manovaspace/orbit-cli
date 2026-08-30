## orbit staff create

Create a staff member (lldap + mailbox)

### Synopsis

Create a staff member and optionally generate a signed onboarding invite token with --invite.

```
orbit staff create [flags]
```

### Options

```
      --forward string           personal forward email (required)
      --groups string            comma-separated groups (default server-side: dev)
  -h, --help                     help for create
      --idempotency-key string   idempotency key (generated if empty)
      --invite                   generate and print a signed onboarding invite token after account creation
      --invite-email string      email address for the invite token (defaults to --forward value)
      --invite-ttl string        invite token TTL (e.g. 7d, 24h, 168h) (default "7d")
      --name string              display name
      --totp                     enroll Authelia TOTP
      --uid string               staff uid (required)
```

### Options inherited from parent commands

```
      --owner-store string   path to owner.json vault
      --server string        staff server URL (or ORBIT_STAFF_URL)
```

### SEE ALSO

* [orbit staff](orbit_staff.md)	 - Manage Orbit staff via the staff control plane

