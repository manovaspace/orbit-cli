---
title: Platform Ownership Verification & Email Delivery
description: How to manage Orbit CLI configuration, verify server ownership via strict out-of-band email OTP challenges, establish the admin root of trust, and configure production Mailcow email delivery for Orbit invitation emails.
---

# Platform Ownership Verification & Email Delivery

This guide explains how to manage Orbit CLI configuration (`~/.config/orbit/config.yaml`), verify server ownership as `alirezaopmc@gmail.com` using strict out-of-band email OTP verification, establish the cryptographic root of trust (`~/.config/orbit/owner.json`), and configure Orbit to dispatch invitation emails through the production Mailcow relay (`mail.manova.space`) so they reach real developer inboxes (Gmail, etc.) with valid SPF/DKIM authentication.

---

## 1. Why this matters & Architecture Overview

By default in a local development environment, `orbit invite create` sends emails to **Mailpit** running on `localhost:10725` — a local mail catcher that prevents tokens from reaching external inboxes.

In production or staging environments:
1. **Configuration Management**: Persistent settings (admin identity, Mailcow SMTP credentials, server endpoint) are stored in `~/.config/orbit/config.yaml` with strict `0600` permissions.
2. **Strict Out-of-Band Ownership Verification**: Before developer invitations can be issued, the platform administrator must verify ownership via a 6-digit OTP challenge dispatched out-of-band to their email. Terminal bypasses are strictly prevented.
3. **Pre-flight SMTP Validation**: `orbit admin init` performs transport pre-flight checks ensuring SMTP credentials and mail relays are fully configured and reachable before attempting OTP challenge delivery.
4. **Root Cryptographic Trust**: On successful OTP verification, Orbit generates a 32-byte cryptographic master signing secret sealed inside `~/.config/orbit/owner.json` (mode `0600`). All subsequent developer invites are cryptographically signed and stamped with the verified owner's identity.

### Configuration Precedence Hierarchy

Orbit resolves settings using the following precedence order (highest to lowest):

```text
┌─────────────────────────────────────────────────────────┐
│ 1. Command-Line Flags    (e.g., --owner, --smtp-host)   │
├─────────────────────────────────────────────────────────┤
│ 2. Environment Variables (e.g., ORBIT_ADMIN_EMAIL)      │
├─────────────────────────────────────────────────────────┤
│ 3. Configuration File    (~/.config/orbit/config.yaml)  │
├─────────────────────────────────────────────────────────┤
│ 4. Built-in Defaults     (mail.manova.space, port 587)  │
└─────────────────────────────────────────────────────────┘
```

---

## 2. Configuration Management (`orbit config`)

Orbit provides the `orbit config` subcommand suite to inspect, initialize, and update persistent CLI configuration in `~/.config/orbit/config.yaml`.

### Initializing Default Configuration

To create the default configuration file:

```bash
orbit config init
```

If the configuration file already exists, use `--force` to overwrite it:

```bash
orbit config init --force
```

### Inspecting Active Configuration

View all configuration settings with sensitive credentials (such as `smtp.pass`) automatically masked:

```bash
orbit config show
```

Output (YAML):
```yaml
server:
  url: https://orbit.manova.space
admin:
  email: alirezaopmc@gmail.com
  name: Alireza
smtp:
  host: mail.manova.space
  port: 587
  user: noreply@manova.space
  pass: '********'
  from: Orbit Platform <noreply@manova.space>
defaults:
  scope: core
  expiry_days: 7
```

To format output as JSON:

```bash
orbit config show --format json
```

### Reading Individual Settings

Retrieve any configuration key value:

```bash
orbit config get admin.email
orbit config get smtp.host
orbit config get server.url
```

### Updating Configuration Settings

Update specific settings in `~/.config/orbit/config.yaml`:

```bash
# Configure administrator identity
orbit config set admin.email alirezaopmc@gmail.com
orbit config set admin.name "Alireza"

# Configure Mailcow SMTP credentials
orbit config set smtp.host mail.manova.space
orbit config set smtp.port 587
orbit config set smtp.user noreply@manova.space
orbit config set smtp.pass "YourSecureMailcowPassword"
orbit config set smtp.from "Orbit Platform <noreply@manova.space>"

# Configure default invite parameters
orbit config set defaults.scope core
orbit config set defaults.expiry_days 7
```

### Locating Configuration File

To view the filesystem path to the active configuration file:

```bash
orbit config path
# Output: /home/opmc/.config/orbit/config.yaml
```

### Supported Configuration Keys

| Key | Description | Default |
|---|---|---|
| `server.url` | Orbit server API endpoint | `https://orbit.manova.space` |
| `admin.email` | Platform administrator email address | `alirezaopmc@gmail.com` |
| `admin.name` | Platform administrator display name | `Alireza` |
| `smtp.host` | Mailcow SMTP server hostname | `mail.manova.space` |
| `smtp.port` | SMTP port (`587` for STARTTLS, `465` for SMTPS) | `587` |
| `smtp.user` | SMTP authentication username | `""` |
| `smtp.pass` | SMTP authentication password (masked in output) | `""` |
| `smtp.from` | Outgoing email sender header | `Orbit Platform <noreply@manova.space>` |
| `defaults.scope` | Default scope for invitation tokens | `core` |
| `defaults.expiry_days` | Default token TTL in days | `7` |

---

## 3. Initializing Server Ownership (`orbit admin init`)

Run this command to establish the platform root of trust:

```bash
orbit admin init --owner alirezaopmc@gmail.com
```

### Pre-flight Checks & Strict Out-of-Band Delivery

1. **Pre-flight SMTP Validation**: Orbit validates that the SMTP mail relay is configured with valid credentials (`ORBIT_SMTP_USER` / `ORBIT_SMTP_PASS` or `smtp.user` / `smtp.pass` in `config.yaml`). If credentials are missing, initialization aborts immediately with instructions to configure SMTP.
2. **OTP Challenge Generation**: A cryptographically random 6-digit verification code is generated (TTL: 10 minutes, max 3 attempts).
3. **Out-of-Band Dispatch**: The OTP challenge is dispatched strictly out-of-band via Mailcow to `alirezaopmc@gmail.com`. The OTP code is **never** printed to the console/terminal.
4. **Interactive Verification**: Orbit prompts the operator to input the 6-digit code received in their inbox.
5. **Vault Sealing**: Upon verification, Orbit generates a 32-byte cryptographic master signing secret and seals the platform vault at `~/.config/orbit/owner.json` with strict `0600` permissions.

Example console session:

```
Orbit Platform Administration Initialization

  ✔  Validated owner email: alirezaopmc@gmail.com
  ✔  Verification code dispatched to alirezaopmc@gmail.com via mail.manova.space:587

  Enter 6-digit verification code: 482910

  ✔  Platform owner alirezaopmc@gmail.com verified and root cryptographic vault sealed.

  Owner Email:       alirezaopmc@gmail.com
  Display Name:      Alireza
  Verified At:       2026-08-27 14:00:00 UTC
  Key Fingerprint:   e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
  Vault File:        ~/.config/orbit/owner.json (0600 sealed)

┌─ OWNERSHIP SECURITY SUMMARY ──────────────────────────────────────────────────┐
│ Master signing key generated and sealed in local vault (mode 0600).           │
│ All developer onboarding invitations and privileged operations               │
│ will be cryptographically signed by this verified owner identity.            │
└───────────────────────────────────────────────────────────────────────────────┘
```

### Non-Interactive / CI Hermetic Testing Mode

For automated continuous integration tests where real email dispatch is not desired, pass both `--no-send` and an explicit `--code`:

```bash
orbit admin init --owner test@example.com --no-send --code 123456
```

> [!NOTE]
> Passing `--no-send` without `--code` will fail pre-flight validation to prevent unintentional unverified vault generation.

---

## 4. Checking Ownership Status (`orbit admin status`)

To check the verification status of the server and inspect vault integrity:

```bash
orbit admin status
```

Output:
```
Orbit Server Ownership Status

  ✔  Platform ownership is VERIFIED.

  Owner Email:       alirezaopmc@gmail.com
  Display Name:      Alireza
  Verified At:       2026-08-27 14:00:00 UTC
  Key Fingerprint:   e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
  Vault Location:    ~/.config/orbit/owner.json
  Permissions:       0600 (secure)
  Mail Gateway:      mail.manova.space:587
```

### JSON Output Format

For scripting or monitoring automation:

```bash
orbit admin status --format json
```

Output:
```json
{
  "verified": true,
  "email": "alirezaopmc@gmail.com",
  "display_name": "Alireza",
  "verified_at": "2026-08-27T14:00:00Z",
  "key_fingerprint": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "vault_location": "/home/opmc/.config/orbit/owner.json",
  "vault_permissions": "0600 (secure)",
  "permissions_valid": true,
  "mail_host": "mail.manova.space:587"
}
```

---

## 5. Completing Verification Separately (`orbit admin verify`)

If you requested an OTP code separately or wish to verify an in-flight challenge:

```bash
orbit admin verify alirezaopmc@gmail.com 482910
```

---

## 6. Rotating Master Signing Secret (`orbit admin rotate-secret`)

If the root signing secret needs rotation (e.g. key lifecycle policy or suspected key compromise), run:

```bash
orbit admin rotate-secret
```

Orbit will prompt for confirmation. For non-interactive execution:

```bash
orbit admin rotate-secret --yes
```

> [!WARNING]
> Rotating the master signing key immediately invalidates all **unclaimed** developer onboarding invite tokens signed with the previous key. Tokens that have already been claimed and onboarded into workspaces remain valid.

---

## 7. Issuing Developer Invitations (`orbit invite create`)

Once ownership is verified, invitations dispatched via `orbit invite create` are automatically:
- **Signed with the master root signing key** from `~/.config/orbit/owner.json`.
- **Stamped with provenance metadata** (`created_by: alirezaopmc@gmail.com`).
- **Dispatched via Mailcow** (`mail.manova.space:587`) directly to the developer's real inbox with HTML and plaintext multipart rendering.

```bash
orbit invite create hardkoding@gmail.com --name "Hard"
```

Output:
```
Orbit Developer Invitation Generated

  ✔  Signed invitation token created for hardkoding@gmail.com
  ✔  Invitation email dispatched to hardkoding@gmail.com via mail.manova.space:587

  Token:          QpSglawu
  Email:          hardkoding@gmail.com
  Created By:     alirezaopmc@gmail.com
  Scope:          core
  Expires:        2026-09-03 14:00:00 UTC (6d 23h)
```

### Suppressing Email Dispatch (Generate Signed Token Only)

```bash
orbit invite create hardkoding@gmail.com --no-send
```

### Local Mailpit Development Mode

For local development without Mailcow credentials or verified ownership:

```bash
orbit invite create test@example.com --insecure
```

This bypasses ownership checks and routes outgoing mail to `localhost:10725` (Mailpit).

---

## 8. Environment Variables Reference

While `~/.config/orbit/config.yaml` is recommended for persistent configuration, the following environment variables are supported and take precedence over the configuration file:

| Variable | Fallback Variable | Description |
|---|---|---|
| `ORBIT_CONFIG` | — | Path to custom YAML configuration file |
| `ORBIT_SERVER` | `ORBIT_SERVER_URL` | Orbit API server URL |
| `ORBIT_ADMIN_EMAIL` | `ORBIT_OWNER_EMAIL` | Administrator owner email |
| `ORBIT_ADMIN_NAME` | `ORBIT_OWNER_NAME` | Administrator display name |
| `ORBIT_SMTP_HOST` | `SMTP_HOST` | Mailcow SMTP host (`mail.manova.space`) |
| `ORBIT_SMTP_PORT` | `SMTP_PORT` | SMTP port (`587` or `465`) |
| `ORBIT_SMTP_USER` | `SMTP_USER` | SMTP authentication username |
| `ORBIT_SMTP_PASS` | `SMTP_PASS` | SMTP authentication password |
| `ORBIT_SMTP_FROM` | `SMTP_FROM` | Outgoing sender email header |

---

## 9. Security Best Practices & Hardening

1. **Vault File Permissions**: Orbit enforces `0600` permissions on `~/.config/orbit/owner.json` and `~/.config/orbit/config.yaml`. Orbit will issue warnings or block execution if vault files are world-readable or group-readable.
2. **Strict Out-of-Band OTP**: Verification codes are never displayed in CLI terminal stdout/stderr during standard initialization to prevent local terminal sniffing attacks.
3. **Challenge Expiration & Rate Limiting**: OTP challenges expire after 10 minutes and are locked after 3 failed verification attempts.
4. **Credential Masking**: The `orbit config show` command masks all sensitive passwords and secrets by default.
5. **No Production Insecure Flag**: The `--insecure` flag should only be used in isolated development environments. In production, verified ownership and authenticated Mailcow SMTP are required.
