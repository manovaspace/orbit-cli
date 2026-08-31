## orbit admin init

Initialize and verify platform server ownership via email OTP challenge

### Synopsis

Initiates server ownership verification. Requests a 6-digit OTP challenge from the Orbit
server, prompts for the code, and seals a 32-byte cryptographic master signing secret in the local
owner vault (mode 0600). Email delivery happens only when the server is orbit-server with SMTP;
orbit-api-gateway stores the OTP in memory and does not send mail.

```
orbit admin init [email] [flags]
```

### Options

```
  -c, --code string     6-digit verification code (for non-interactive execution)
  -f, --force           Force re-initialization even if already verified
  -h, --help            help for init
  -n, --name string     Owner display name (e.g. 'Alex Smith')
      --no-send         Suppress dispatching challenge email
  -o, --owner string    Owner email address (e.g. admin@example.com)
  -s, --server string   Orbit server URL (e.g. https://orbit.manova.space)
      --store string    Custom path to owner storage vault file
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit admin](orbit_admin.md)	 - Manage platform ownership verification and root cryptographic secrets

