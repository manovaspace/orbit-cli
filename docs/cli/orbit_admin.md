## orbit admin

Manage platform ownership verification and root cryptographic secrets

### Synopsis

Commands to verify server and platform administrative ownership, manage
cryptographic root signing secrets, and check ownership vault status.

### Options

```
  -h, --help   help for admin
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit admin grant](orbit_admin_grant.md)	 - Generate an 8-digit single-use authorization code for a new admin
* [orbit admin init](orbit_admin_init.md)	 - Initialize and verify platform server ownership via email OTP challenge
* [orbit admin rotate-secret](orbit_admin_rotate-secret.md)	 - Rotate root master signing secret in the sealed owner vault
* [orbit admin status](orbit_admin_status.md)	 - Display platform ownership verification status, vault integrity, and mail config
* [orbit admin totp](orbit_admin_totp.md)	 - Manage two-factor authentication and user TOTP recovery
* [orbit admin verify](orbit_admin_verify.md)	 - Seal owner.json locally (hermetic OTP; no API call)

