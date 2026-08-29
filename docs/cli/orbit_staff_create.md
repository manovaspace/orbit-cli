## orbit staff create

Create a staff member (lldap + mailbox)

```
orbit staff create [flags]
```

### Options

```
      --forward string           personal forward email (required)
      --groups string            comma-separated groups (default server-side: dev)
  -h, --help                     help for create
      --idempotency-key string   idempotency key (generated if empty)
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

