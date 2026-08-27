---
title: Platform Ownership Verification & Email Delivery
description: How to verify server ownership, establish the admin root of trust, and configure production Mailcow email delivery for Orbit invitation emails.
---

# Platform Ownership Verification & Email Delivery

This guide explains how to verify server ownership as `alirezaopmc@gmail.com` and configure Orbit to dispatch invitation emails through the production Mailcow relay (`mail.manova.space`) so they reach real inboxes (Gmail, etc.) rather than the local development mail trap.

---

## Why this matters

By default in a development environment, `orbit invite create` sends emails to **Mailpit** running on `localhost:10725` — a local mail catcher that prevents tokens from reaching external inboxes.

To send invites to real developers:
1. **Verify ownership** of this server with your email address (`alirezaopmc@gmail.com`).
2. **Configure Mailcow SMTP** credentials so emails route through `mail.manova.space` with valid DKIM/SPF.

---

## 1. Initializing server ownership (`orbit admin init`)

Run this **once** on the server after installing Orbit:

```bash
orbit admin init --owner alirezaopmc@gmail.com
```

**What happens:**
1. Orbit generates a secure 6-digit OTP challenge (TTL: 10 minutes, max 3 attempts).
2. Sends the OTP to `alirezaopmc@gmail.com` via `mail.manova.space` (or the configured SMTP host).
3. Prompts you to enter the 6-digit code you received.
4. On success, generates a 32-byte cryptographic master signing secret and seals the **platform root vault** at `~/.config/orbit/owner.json` with `0600` permissions.

```
Orbit Platform Administration Initialization

  ✔  Validated owner email: alirezaopmc@gmail.com
  ✔  Verification code dispatched to alirezaopmc@gmail.com via mail.manova.space:587

  Enter 6-digit verification code: 482910

  ✔  Verification successful!
  ✔  Generated master platform signing secret.
  ✔  Sealed platform vault to ~/.config/orbit/owner.json (mode 0600)

Server is now verified and owned by alirezaopmc@gmail.com.
```

### Non-interactive / CI mode

```bash
orbit admin init --owner alirezaopmc@gmail.com --no-send --code 482910
```

---

## 2. Checking admin status (`orbit admin status`)

```bash
orbit admin status
```

Output:
```
Orbit Platform Authority Status

  Owner Email:    alirezaopmc@gmail.com
  Status:         ✔ Verified (2026-08-26 22:45:00 UTC)
  Vault:          ~/.config/orbit/owner.json
  Permissions:    ✔ Secure (0600)
  Mail Gateway:   mail.manova.space:587 (STARTTLS)
```

---

## 3. Issuing developer invitations (`orbit invite create`)

Once ownership is verified, all invites are automatically:
- **Signed with the master secret** (from the vault, not the dev fallback).
- **Stamped with `created_by: alirezaopmc@gmail.com`** for full provenance.
- **Dispatched via Mailcow** to the developer's real inbox.

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
  Expires:        2026-09-02 22:35:00 UTC (6d 23h)
```

### Suppressing email dispatch (generate token only)

```bash
orbit invite create hardkoding@gmail.com --no-send
```

### Using local Mailpit (dev/testing)

```bash
orbit invite create test@example.com --insecure
```
This bypasses ownership verification and routes to `localhost:10725` (Mailpit).

---

## 4. Configuring SMTP credentials

The mailer reads from environment variables (set in your `.env` or system environment):

| Variable | Default | Description |
|---|---|---|
| `ORBIT_SMTP_HOST` | `mail.manova.space` | SMTP server hostname |
| `ORBIT_SMTP_PORT` | `587` | SMTP port (587=STARTTLS, 465=TLS) |
| `ORBIT_SMTP_USER` | — | SMTP auth username |
| `ORBIT_SMTP_PASS` | — | SMTP auth password |
| `ORBIT_SMTP_FROM` | `Orbit Platform <noreply@manova.space>` | Sender address |

Or pass them as CLI flags:
```bash
orbit invite create dev@example.com \
  --smtp-host mail.manova.space \
  --smtp-port 587 \
  --smtp-from "Orbit <noreply@manova.space>"
```

---

## 5. Rotating the master signing secret (`orbit admin rotate-secret`)

After rotating, existing **unclaimed** tokens signed with the old secret become invalid. Claimed tokens already consumed are unaffected.

```bash
orbit admin rotate-secret
# or non-interactively:
orbit admin rotate-secret --yes
```

---

## 6. Security notes

- The vault file (`~/.config/orbit/owner.json`) is created with `0600` permissions — only readable by the owner.
- Orbit will refuse to proceed if the vault file has been made world-readable.
- OTP challenges expire in **10 minutes** and are invalidated after **3 failed attempts**.
- The `--insecure` flag bypasses ownership verification for development only — never use in production.
