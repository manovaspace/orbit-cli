## orbit staff reset-password

Rotate the LLDAP password used for staff SSO and mailbox sign-in

```
orbit staff reset-password <uid> [flags]
```

### Options

```
  -h, --help      help for reset-password
      --ldap      reset the shared LLDAP password used for SSO and mail
      --mailbox   deprecated alias for the shared LLDAP password
```

### Options inherited from parent commands

```
      --config string        Custom path to Orbit CLI configuration file
      --owner-store string   path to owner.json vault
      --server string        staff server URL (or ORBIT_STAFF_URL)
```

### SEE ALSO

* [orbit staff](orbit_staff.md)	 - Manage Orbit staff via the staff control plane

