## orbit invite

Manage developer onboarding invite tokens

### Synopsis

Create, list, and revoke cryptographically signed developer onboarding invitations.
Invitations are HMAC-SHA256 signed single-use or scoped tokens with built-in expiration
and claims for automated developer onboarding.

### Options

```
  -h, --help   help for invite
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit invite create](orbit_invite_create.md)	 - Generate a cryptographically signed onboarding invitation token
* [orbit invite list](orbit_invite_list.md)	 - List active and stored developer onboarding invitations
* [orbit invite revoke](orbit_invite_revoke.md)	 - Revoke an active developer onboarding invitation

