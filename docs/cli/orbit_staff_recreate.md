## orbit staff recreate

Deprecated: use the safe Keycloak identity reset procedure

### Synopsis

This command no longer deletes or recreates staff identities. Keycloak retains federated user profiles and Google/passkey credentials after an LLDAP deletion. Before deleting the LLDAP identity, deploy remove-keycloak-staff-user.py on the identity host and require its --uid <uid> --preflight check to succeed. Stop if it is unavailable or preflight fails. Then follow the staff lifecycle guide: delete the LLDAP identity, review a cleanup dry run, execute with --confirm <uid>, and create only after cleanup succeeds. Mailbox contents remain preserved.

```
orbit staff recreate [flags]
```

### Options

```
      --forward string           legacy flag; ignored because recreate is disabled
      --groups string            legacy flag; ignored because recreate is disabled
  -h, --help                     help for recreate
      --idempotency-key string   legacy flag; ignored because recreate is disabled
      --invite                   legacy flag; ignored because recreate is disabled
      --invite-email string      legacy flag; ignored because recreate is disabled
      --invite-ttl string        legacy flag; ignored because recreate is disabled (default "7d")
      --name string              legacy flag; ignored because recreate is disabled
      --no-send                  legacy flag; ignored because recreate is disabled
      --uid string               UID to include in the safe reset instructions (legacy flag)
```

### Options inherited from parent commands

```
      --config string        Custom path to Orbit CLI configuration file
      --owner-store string   path to owner.json vault
      --server string        staff server URL (or ORBIT_STAFF_URL)
```

### SEE ALSO

* [orbit staff](orbit_staff.md)	 - Manage Orbit staff via the staff control plane

