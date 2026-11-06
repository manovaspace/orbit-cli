## orbit staff delete

Delete a staff member

### Synopsis

Delete the staff member's LLDAP identity. The Stalwart mailbox, messages, and forwarding remain, but Keycloak browser sign-in and LLDAP-backed mail authentication stop. This command does not remove the imported Keycloak profile or validate migration backups; use the reviewed identity-reset procedure for a full reset.

```
orbit staff delete <uid> [flags]
```

### Options

```
  -h, --help   help for delete
      --json   Print the result as a JSON envelope
```

### Options inherited from parent commands

```
      --config string        Custom path to Orbit CLI configuration file
      --owner-store string   path to owner.json vault
      --server string        staff server URL (or ORBIT_STAFF_URL)
```

### SEE ALSO

* [orbit staff](orbit_staff.md)	 - Manage Orbit staff via the staff control plane

