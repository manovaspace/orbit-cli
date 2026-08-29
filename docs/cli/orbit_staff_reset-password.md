## orbit staff reset-password

Rotate ldap and/or mailbox passwords, optionally Authelia TOTP

```
orbit staff reset-password <uid> [flags]
```

### Options

```
  -h, --help      help for reset-password
      --ldap      reset SSO/ldap password
      --mailbox   reset mailbox password
      --totp      replace Authelia TOTP and print otpauth
```

### Options inherited from parent commands

```
      --owner-store string   path to owner.json vault
      --server string        staff server URL (or ORBIT_STAFF_URL)
```

### SEE ALSO

* [orbit staff](orbit_staff.md)	 - Manage Orbit staff via the staff control plane

