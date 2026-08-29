## orbit invite create

Generate a cryptographically signed onboarding invitation token

### Synopsis

Generates a HMAC-SHA256 signed invite token for a developer email address with specified scope and expiration.

```
orbit invite create [email] [flags]
```

### Options

```
      --email string         Developer email address
  -e, --expires string       Expiration duration (e.g. '7d', '24h', '168h') (default "168h")
  -h, --help                 help for create
      --insecure             Bypass owner verification check (development only)
  -i, --interactive          Interactive wizard for generating developer invitations
  -n, --name string          Display name of the developer (e.g. 'Alex Smith')
      --no-send              Suppress dispatching onboarding invitation email
      --owner-store string   Custom path to owner storage vault file (default: $ORBIT_OWNER_STORE or ~/.config/orbit/owner.json)
  -s, --scope string         Access scope (e.g. 'core', 'client', 'guest') (default "core")
      --secret string        Raw signing secret (overrides --secret-env)
      --secret-env string    Environment variable containing signing secret (default "ORBIT_INVITE_SECRET")
  -m, --send                 Dispatch onboarding invitation email via SMTP (default: true) (default true)
      --smtp-from string     Sender email address (default: $ORBIT_SMTP_FROM)
      --smtp-host string     SMTP server host (default: $ORBIT_SMTP_HOST or mail.manova.space)
      --smtp-port string     SMTP server port (default: $ORBIT_SMTP_PORT or 587)
      --store-file string    Custom path to invites storage file
```

### SEE ALSO

* [orbit invite](orbit_invite.md)	 - Manage developer onboarding invite tokens

