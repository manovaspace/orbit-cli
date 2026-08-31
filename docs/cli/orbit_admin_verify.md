## orbit admin verify

Seal owner.json locally (hermetic OTP; no API call)

### Synopsis

Local-only verification. The code is checked in-process, not against orbit-server or the gateway. On success a new master secret is sealed in owner.json. Does not import a server fingerprint.

```
orbit admin verify [email] [code] [flags]
```

### Options

```
  -c, --code string    6-digit verification code
  -f, --force          Force re-verification even if already verified
  -h, --help           help for verify
  -n, --name string    Owner display name
  -o, --owner string   Owner email address
      --store string   Custom path to owner storage vault file
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit admin](orbit_admin.md)	 - Manage platform ownership verification and root cryptographic secrets

