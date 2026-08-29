## orbit admin rotate-secret

Rotate root master signing secret in the sealed owner vault

### Synopsis

Generates a fresh 32-byte cryptographic root signing secret and seals the vault.
WARNING: All developer onboarding invitation tokens signed with the previous secret will be invalidated.

```
orbit admin rotate-secret [flags]
```

### Options

```
  -h, --help           help for rotate-secret
      --store string   Custom path to owner storage vault file
  -y, --yes            Skip interactive confirmation prompt
```

### SEE ALSO

* [orbit admin](orbit_admin.md)	 - Manage platform ownership verification and root cryptographic secrets

