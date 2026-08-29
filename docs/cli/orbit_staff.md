## orbit staff

Manage Orbit staff via the staff control plane

### Synopsis

Create, list, update, disable, enable, delete, recreate, and reset passwords for staff accounts through orbit-staff (HMAC). Reserved directory accounts are rejected: admin, authelia-bind, verdaccio-bind, verdaccio-ci.

### Options

```
  -h, --help                 help for staff
      --owner-store string   path to owner.json vault
      --server string        staff server URL (or ORBIT_STAFF_URL)
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit staff create](orbit_staff_create.md)	 - Create a staff member (lldap + mailbox)
* [orbit staff delete](orbit_staff_delete.md)	 - Delete a staff member
* [orbit staff disable](orbit_staff_disable.md)	 - Disable a staff member
* [orbit staff enable](orbit_staff_enable.md)	 - Enable a staff member
* [orbit staff get](orbit_staff_get.md)	 - Get a staff member
* [orbit staff list](orbit_staff_list.md)	 - List staff members
* [orbit staff recreate](orbit_staff_recreate.md)	 - Delete then create a staff member (fresh SSO + mailbox)
* [orbit staff reset-password](orbit_staff_reset-password.md)	 - Rotate ldap and/or mailbox passwords, optionally Authelia TOTP
* [orbit staff update](orbit_staff_update.md)	 - Update display name, forward, or groups

