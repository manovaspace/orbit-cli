---
title: Platform Ownership Verification & Email Delivery
description: Complete guide to Orbit dual-binary architecture (orbit client vs orbit-server daemon), pure API client workflow, out-of-band email OTP challenges (/api/v1/admin/challenge), cryptographic root-of-trust vault sealing, and container/systemd deployments.
---

# Platform Ownership Verification & Email Delivery

This guide explains the architecture and operation of Orbit's platform ownership verification system, the dual-binary model (`orbit` client on developer workstations vs `orbit-server` daemon on infrastructure), the pure API client workflow in `orbit admin init`, out-of-band email OTP challenges dispatched via Stalwart (`mail.manova.space`), and cryptographic root vault management (`~/.config/orbit/owner.json`).

**Platform owner runbook (matches current code):** `handbook/docs/orbit/guides/orbit-admin-init.md`. Staff directory CLI: `handbook/docs/orbit/guides/staff-lifecycle.md`.

### Current implementation (code vs this page)

Treat sections below as the **intended** `orbit-server` design. As of this writing:

| Topic | What the code does |
| --- | --- |
| Default URL `https://orbit.manova.space` | Caddy → **`orbit-api-gateway:10120`**, not `orbit-server` |
| Gateway `POST …/ownership/challenge` | In-memory OTP only — **no SMTP / no notifications** |
| `orbit admin init` after verify | Generates a **new** local secret; does not import the server fingerprint |
| `orbit admin status` / `rotate-secret` / `verify` | Local vault only (`verify` is hermetic; it does not call the API) |
| `orbit invite create` | Signs locally from `owner.json` and SMTP-sends from the workstation (not the Orbit HTTPS API) |
| `orbit staff` | HMAC client to `https://staff.dev.manova.space` (`ORBIT_STAFF_URL`); needs verified `owner.json`. Includes `recreate` and `reset-password --totp`. Server not live. |

---

## 1. Why This Matters & Architecture Overview

Orbit uses a **dual-binary architecture** that decouples developer workstation tools from server infrastructure:

- **Developer Workstations (`orbit`)**: The CLI used by developers and administrators for local development (`orbit dev`), workspace onboarding (`orbit onboard`), invitation management (`orbit invite`), staff directory (`orbit staff`), and platform administrative initialization (`orbit admin init`). Workstations do not hold lldap or Stalwart *admin* credentials. **Exception:** `orbit invite create` SMTP-sends from this machine unless `--no-send`.
- **Server Infrastructure (`orbit-server`)**: The dedicated, headless HTTP edge daemon running on production or staging servers (bare metal, systemd, or containerized). It holds Stalwart SMTP credentials, handles OTP challenge lifecycles with rate limiting, verifies administrator ownership, and provisions developer claims.

### Architecture Highlights

1. **Dual-Binary Separation**: Workstations run `orbit` (pure API client); servers run `orbit-server` (infrastructure daemon).
2. **Pure API Client Architecture**: `orbit admin init` talks HTTPS to the Orbit API for the ownership challenge. `orbit staff` talks HMAC HTTPS to orbit-staff. `orbit invite create` is **not** an API client — it signs locally and may SMTP-send from the workstation.
3. **Dedicated Server Daemon (`orbit-server`)**: Runs on production/staging infrastructure, holds backend Stalwart SMTP credentials, manages OTP challenges with rate limiting and expiration, and exposes REST endpoints.
4. **Strict Out-of-Band Ownership Verification**: Platform administrators must verify ownership via a 6-digit OTP challenge sent out-of-band to their email address (`admin@manova.space`). Verification codes are **never** logged to stdout/stderr in standard mode.
5. **Root Cryptographic Trust**: Upon remote API verification, Orbit generates a 32-byte cryptographic master signing secret sealed inside `~/.config/orbit/owner.json` (mode `0600`). All subsequent developer invites are cryptographically signed and stamped with the verified owner's identity.

### End-to-End Ownership Verification Flow

**Today (code):** Caddy on `https://orbit.manova.space` terminates at **orbit-api-gateway**. The challenge OTP is **in-memory on the gateway** — it is not emailed. `orbit admin verify` is hermetic/local and does not call the API. After a successful local verify, the CLI seals a **new** `owner.json` secret.

```text
┌──────────────┐         ┌───────────────┐         ┌─────────────────────────┐
│   Operator   │         │   Orbit CLI   │         │ orbit.manova.space      │
│  (Terminal)  │         │ (orbit admin) │         │ (api-gateway, in-memory │
└──────┬───────┘         └───────┬───────┘         │  OTP — no SMTP)         │
       │                         │                 └────────────┬────────────┘
       │  orbit admin init       │                              │
       ├────────────────────────>│                              │
       │                         │ POST …/ownership/challenge   │
       │                         ├─────────────────────────────>│
       │                         │ 200 OK (OTP stored in RAM)   │
       │                         │<─────────────────────────────┤
       │ Prompts for 6-digit OTP │                              │
       │<────────────────────────┤                              │
       │ Enters code             │                              │
       ├────────────────────────>│                              │
       │                         │ local hermetic verify        │
       │                         │ (no API) + seal owner.json   │
       │ Display Success Card    │                              │
       │<────────────────────────┤                              │
```

**Intended later:** `orbit-server` holds Stalwart SMTP and emails the OTP (`POST /api/v1/admin/challenge` → mail). Until that ships, do not expect a challenge email.

---

## 2. Server Edge Daemon (`orbit-server`)

The `orbit-server` daemon runs the HTTP onboarding API, handles developer claim requests, manages challenge lifecycles, and dispatches transactional emails via SMTP.

### Starting the Server Daemon Directly

```bash
# Start server with default port (:8080)
orbit-server

# With custom listen address
orbit-server --addr :8080

# With explicit SMTP credentials and custom signing secret
orbit-server \
  --addr :8080 \
  --smtp-host mail.manova.space \
  --smtp-port 587 \
  --smtp-user noreply@manova.space \
  --smtp-pass "$SMTP_PASSWORD" \
  --smtp-from "Orbit Platform <noreply@manova.space>" \
  --signing-secret "$ORBIT_SIGNING_SECRET"
```

### Running as a systemd Service

For persistent VPS and production server deployments, manage `orbit-server` via systemd:

1. Create `/etc/systemd/system/orbit-server.service`:

```ini
[Unit]
Description=Orbit Platform Infrastructure Daemon
Documentation=https://orbit.manova.space/docs
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=orbit
Group=orbit
EnvironmentFile=-/etc/orbit/orbit-server.env
ExecStart=/usr/local/bin/orbit-server --addr :8080
Restart=always
RestartSec=5s
LimitNOFILE=65535
AmbientCapabilities=CAP_NET_BIND_SERVICE
ProtectSystem=full
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
```

2. Configure environment file at `/etc/orbit/orbit-server.env`:

```bash
ORBIT_SIGNING_SECRET=your-32-byte-master-signing-secret
ORBIT_SMTP_HOST=mail.manova.space
ORBIT_SMTP_PORT=587
ORBIT_SMTP_USER=noreply@manova.space
ORBIT_SMTP_PASS=your-smtp-password
ORBIT_SMTP_FROM=Orbit Platform <noreply@manova.space>
```

3. Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now orbit-server
sudo systemctl status orbit-server
```

### Running via Docker and Docker Compose

Orbit provides an official multi-stage `Dockerfile` producing a minimal, secure Alpine container runtime for `orbit-server`.

#### Direct Docker Run

```bash
# Build the image
docker build -t orbit-server:latest -f Dockerfile .

# Run the container
docker run -d \
  --name orbit-server \
  --restart unless-stopped \
  -p 8080:8080 \
  -e ORBIT_SIGNING_SECRET="$ORBIT_SIGNING_SECRET" \
  -e ORBIT_SMTP_HOST="mail.manova.space" \
  -e ORBIT_SMTP_PORT="587" \
  -e ORBIT_SMTP_USER="noreply@manova.space" \
  -e ORBIT_SMTP_PASS="$SMTP_PASSWORD" \
  -e ORBIT_SMTP_FROM="Orbit Platform <noreply@manova.space>" \
  orbit-server:latest
```

#### Docker Compose Deployment

Add `orbit-server` to your `docker-compose.yml`:

```yaml
version: '3.8'

services:
  orbit-server:
    image: ghcr.io/manovaspace/orbit-server:latest
    build:
      context: .
      dockerfile: Dockerfile
    container_name: orbit-server
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - ORBIT_SIGNING_SECRET=${ORBIT_SIGNING_SECRET}
      - ORBIT_SMTP_HOST=mail.manova.space
      - ORBIT_SMTP_PORT=587
      - ORBIT_SMTP_USER=noreply@manova.space
      - ORBIT_SMTP_PASS=${SMTP_PASSWORD}
      - ORBIT_SMTP_FROM=Orbit Platform <noreply@manova.space>
    volumes:
      - orbit-data:/root/.config/orbit
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 5s

volumes:
  orbit-data:
    driver: local
```

### Daemon Command-Line Flags

| Flag | Shorthand | Description | Default |
|---|---|---|---|
| `--addr` | `-a` | HTTP server listen address | `:8080` |
| `--smtp-host` | | SMTP mail relay host | `mail.manova.space` |
| `--smtp-port` | | SMTP mail relay port | `587` |
| `--smtp-user` | | SMTP authentication username | `""` |
| `--smtp-pass` | | SMTP authentication password | `""` |
| `--smtp-from` | | Outgoing email sender header | `Orbit Platform <noreply@manova.space>` |
| `--signing-secret` | | Cryptographic secret for signing/verification | Sealed vault or env |
| `--store` | | Path to invites storage JSON file | `~/.config/orbit/invites.json` |
| `--owner-store` | | Path to owner vault JSON file | `~/.config/orbit/owner.json` |
| `--config` | | Custom path to configuration file | `~/.config/orbit/config.yaml` |

### Signing Secret Resolution Order

The server resolves its cryptographic signing secret in the following priority:
1. `--signing-secret` CLI flag
2. Environment variables: `ORBIT_SIGNING_SECRET`, `ORBIT_INVITE_SECRET`, `ORBIT_JWT_SECRET`
3. Sealed owner vault (`~/.config/orbit/owner.json`)
4. Development fallback secret (with security warning in logs)

### Graceful Shutdown

The daemon catches `SIGINT` (Ctrl+C) and `SIGTERM` signals, executing a 5-second graceful shutdown context to complete in-flight HTTP requests before closing listeners.

---

## 3. Server Admin API Endpoints

The server exposes dedicated endpoints for platform ownership verification and developer onboarding.

### 1. Initiate Admin Challenge (`POST /api/v1/admin/challenge`)

Generates a cryptographically random 6-digit OTP code, stores it in memory (TTL: 10 minutes, max 3 attempts), and dispatches the challenge email via Stalwart.

* **Path**: `/api/v1/admin/challenge` (alias: `/api/v1/system/ownership/challenge`)
* **Method**: `POST`
* **Content-Type**: `application/json`

**Request Body**:
```json
{
  "email": "admin@manova.space"
}
```

**Success Response (`200 OK`)**:
```json
{
  "status": "challenge_sent",
  "email": "admin@manova.space",
  "expires_at": "2026-08-27T14:10:00Z",
  "message": "verification OTP sent to admin@manova.space"
}
```

**Error Responses**:
* `400 Bad Request`: Missing or malformed email address.
* `429 Too Many Requests`: IP rate limit exceeded (includes `Retry-After` header).
* `500 Internal Server Error`: SMTP transport failure or mailer unconfigured.

### 2. Verify Admin OTP (`POST /api/v1/admin/verify`)

Validates the provided 6-digit OTP code against the active challenge for the email.

* **Path**: `/api/v1/admin/verify` (alias: `/api/v1/system/ownership/verify`)
* **Method**: `POST`
* **Content-Type**: `application/json`

**Request Body**:
```json
{
  "email": "admin@manova.space",
  "code": "482910",
  "display_name": "Alireza"
}
```

**Success Response (`200 OK`)**:
```json
{
  "status": "verified",
  "email": "admin@manova.space",
  "display_name": "Alireza",
  "key_fingerprint": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "verified_at": "2026-08-27T14:00:00Z",
  "message": "platform ownership successfully verified for admin@manova.space"
}
```

**Error Responses**:
* `400 Bad Request`: Incorrect OTP code, expired challenge, or exceeded max attempts (3).
* `429 Too Many Requests`: Rate limit exceeded.

### 3. Additional Server Endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` or `/health` | `GET` | Server liveness and provisioner health probe |
| `/v1/onboard/claim` | `POST` | Claim developer invitation token & provision workspace |
| `/api/v1/dev/onboard/rollback` | `POST` | Rollback provisioned developer workspace |

---

## 4. Configuration Management (`orbit config`)

Orbit manages client-side settings in `~/.config/orbit/config.yaml` with strict `0600` permissions (Version 2 schema with zero plaintext secrets on disk).

### Initializing Configuration

```bash
# Initialize default configuration
orbit config init

# Overwrite existing config
orbit config init --force
```

### Inspecting Active Configuration

View all configuration settings with masked values:

```bash
orbit config show
```

Output (YAML):
```yaml
version: 2
server:
  url: https://orbit.manova.space
  timeout: 30s
staff:
  url: https://staff.dev.manova.space
assets:
  bucket: manova-assets
  endpoint: https://assets.manova.space
  auto_pull: true
defaults:
  scope: core
  expiry_days: 7
ui:
  color: true
  output: text
custom: {}
```

To output in JSON format:
```bash
orbit config show --format json
```

### Reading and Updating Individual Settings

```bash
# Read values
orbit config get server.url
orbit config get defaults.scope

# Read raw value for scripting
orbit config get server.url --raw

# Update server endpoint and defaults
orbit config set server.url https://orbit.manova.space
orbit config set defaults.scope core
orbit config set custom.team.lead "Alireza"

# Inspect active file path
orbit config path
# Output: /home/opmc/.config/orbit/config.yaml

# List all resolved configurations with provenance sources
orbit config list
```

### Configuration Keys Reference

| Key | Type | Description | Default |
|---|---|---|---|
| `server.url` | string | Orbit API server endpoint URL | `https://orbit.manova.space` |
| `server.timeout` | duration | API client request timeout | `30s` |
| `staff.url` | string | Staff control plane API URL | `https://staff.dev.manova.space` |
| `assets.bucket` | string | Cloudflare R2 bucket for private assets | `manova-assets` |
| `assets.endpoint` | string | R2 asset CDN endpoint URL | `https://assets.manova.space` |
| `assets.auto_pull` | bool | Automatically pull missing assets on dev startup | `true` |
| `defaults.scope` | string | Default scope for invitation tokens | `core` |
| `defaults.expiry_days` | int | Default invite TTL in days | `7` |
| `ui.color` | bool | Enable ANSI colored CLI output | `true` |
| `ui.output` | string | Default output format (`text`, `json`, `yaml`) | `text` |
| `custom.*` | dynamic | Arbitrary user-defined key-value configuration tree | `{}` |

> [!NOTE]
> Workstation secrets (e.g. root master signing key) are sealed exclusively in `~/.config/orbit/owner.json` via `orbit admin init`. Server daemon SMTP credentials (`$ORBIT_SMTP_*` / `$SMTP_*` / `--smtp-*`) are managed exclusively on the server host and never stored in workstation `config.yaml`.

---

## 5. Initializing Platform Ownership (`orbit admin init`)

Run `orbit admin init` to establish the platform root of cryptographic trust.

```bash
orbit admin init --owner admin@manova.space
```

### Execution Steps

1. **Server API Connection**: Resolves the API endpoint (`cfg.Server.URL`, `--server`, or `ORBIT_SERVER`).
2. **Challenge Request**: Calls `POST /api/v1/admin/challenge` on the server.
3. **Out-of-Band Dispatch**: The server generates an OTP and dispatches it via Stalwart to `admin@manova.space`.
4. **Interactive Code Entry**: Orbit prompts the operator in the terminal for the 6-digit OTP.
5. **API Verification**: Calls `POST /api/v1/admin/verify` to validate the code with the server.
6. **Local Vault Sealing**: Orbit generates a 32-byte cryptographic master signing secret and seals it into `~/.config/orbit/owner.json` (mode `0600`).

### Interactive Console Session Example

```text
Orbit Platform Administration Initialization

  ➜  Connecting to Orbit server at https://orbit.manova.space...
  ✔  Challenge accepted for admin@manova.space (OTP is emailed only when the server is orbit-server with SMTP)

  Enter 6-digit verification code: 482910

Orbit Platform Ownership Verified
  ✔  Platform owner admin@manova.space verified and root cryptographic vault sealed.

  Owner Email:       admin@manova.space
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

### Non-Interactive / CI Hermetic Test Mode

For offline development, unit tests, or CI pipelines where remote API calls or email dispatch are not desired:

```bash
orbit admin init --owner test@example.com --no-send --code 123456
```

> [!NOTE]
> Passing `--no-send` without an explicit `--code` will fail immediately to prevent unintentional unverified vault generation.

---

## 6. Checking Ownership Status (`orbit admin status`)

Inspect whether platform ownership is verified, verify vault file permissions, and check active server settings:

```bash
orbit admin status
```

Output:
```text
Orbit Server Ownership Status

  ✔  Platform ownership is VERIFIED.

  Owner Email:       admin@manova.space
  Display Name:      Alireza
  Verified At:       2026-08-27 14:00:00 UTC
  Key Fingerprint:   e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
  Vault Location:    ~/.config/orbit/owner.json
  Permissions:       0600 (secure)
  Mail Gateway:      mail.manova.space:587
```

### Scripting & Monitoring (JSON Format)

```bash
orbit admin status --format json
```

Output:
```json
{
  "verified": true,
  "email": "admin@manova.space",
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

## 7. Completing Verification Separately (`orbit admin verify`)

This command is **hermetic**: it does not call `orbit.manova.space`. Any well-formed 6-digit code is accepted in-process, then a **new** local secret is sealed. Use it to finish a local vault when you are not relying on emailed OTP.

```bash
orbit admin verify admin@manova.space 482910
```

---

## 8. Rotating Master Signing Secret (`orbit admin rotate-secret`)

To rotate the cryptographic root signing key (e.g. key lifecycle policy or suspected key compromise):

```bash
orbit admin rotate-secret
```

For non-interactive automation:
```bash
orbit admin rotate-secret --yes
```

> [!WARNING]
> Rotating the master signing key immediately invalidates all **unclaimed** developer onboarding invite tokens signed with the previous key. Tokens that have already been claimed and onboarded into workspaces remain valid.

---

## 9. Issuing Developer Invitations (`orbit invite create`)

Once ownership is verified, invitations dispatched via `orbit invite create` are automatically:
- **Signed with the master root signing key** from `~/.config/orbit/owner.json`.
- **Stamped with provenance metadata** (`created_by: admin@manova.space`).
- **Dispatched via Stalwart** (`mail.manova.space:587`) directly to the developer's real inbox with HTML and plaintext multipart rendering.

```bash
orbit invite create hardkoding@gmail.com --name "Hard"
```

Output:
```text
Orbit Developer Invitation Generated

  ✔  Signed invitation token created for hardkoding@gmail.com
  ✔  Invitation email dispatched to hardkoding@gmail.com via mail.manova.space:587

  Token:          QpSglawu
  Email:          hardkoding@gmail.com
  Created By:     admin@manova.space
  Scope:          core
  Expires:        2026-09-03 14:00:00 UTC (6d 23h)
```

### Suppressing Email Dispatch (Generate Signed Token Only)

```bash
orbit invite create hardkoding@gmail.com --no-send
```

### Local Mailpit Development Mode

For local development without Stalwart credentials or verified ownership:

```bash
orbit invite create test@example.com --insecure
```

This bypasses ownership checks and routes outgoing mail to `localhost:10725` (Mailpit).

---

## 10. Environment Variables Reference

Environment variables take precedence over configuration file settings:

### Client Environment Variables

| Variable | Fallback Variable | Description | Default |
|---|---|---|---|
| `ORBIT_CONFIG` | — | Path to custom YAML configuration file | `~/.config/orbit/config.yaml` |
| `ORBIT_SERVER` | `ORBIT_SERVER_URL` | Orbit API server URL | `https://orbit.manova.space` |
| `ORBIT_ADMIN_EMAIL` | `ORBIT_OWNER_EMAIL` | Administrator owner email | `admin@manova.space` |
| `ORBIT_ADMIN_NAME` | `ORBIT_OWNER_NAME` | Administrator display name | `Alireza` |
| `ORBIT_STAFF_URL` | — | orbit-staff HMAC API (`orbit staff --server`) | `https://staff.dev.manova.space` |

### Server Daemon Environment Variables

| Variable | Fallback Variable | Description | Default |
|---|---|---|---|
| `ORBIT_SIGNING_SECRET` | `ORBIT_INVITE_SECRET`, `ORBIT_JWT_SECRET` | Master cryptographic signing secret | Sealed vault |
| `ORBIT_SMTP_HOST` | `SMTP_HOST` | Stalwart SMTP host | `mail.manova.space` |
| `ORBIT_SMTP_PORT` | `SMTP_PORT` | SMTP port (`587` or `465`) | `587` |
| `ORBIT_SMTP_USER` | `SMTP_USER` | SMTP authentication username | `""` |
| `ORBIT_SMTP_PASS` | `SMTP_PASS` | SMTP authentication password | `""` |
| `ORBIT_SMTP_FROM` | `SMTP_FROM` | Outgoing sender email header | `Orbit Platform <noreply@manova.space>` |

---

## 11. Security Best Practices & Hardening

1. **Zero Client SMTP Exposure for admin credentials**: Developer machines do not store Stalwart *admin* passwords. `orbit invite create` still SMTP-sends from the workstation unless `--no-send`.
2. **Vault File Permissions**: Orbit enforces `0600` permissions on `~/.config/orbit/owner.json` and `~/.config/orbit/config.yaml`. Orbit will issue warnings or block execution if vault files are world-readable or group-readable.
3. **Strict Out-of-Band OTP**: Verification codes are never displayed in CLI terminal stdout/stderr during standard initialization to prevent local terminal sniffing attacks.
4. **Challenge Expiration & Rate Limiting**: OTP challenges expire after 10 minutes and are locked after 3 failed verification attempts. The server enforces sliding window rate limiting (10 req/min default per IP).
5. **Credential Masking**: The `orbit config show` command masks all sensitive passwords and secrets by default.
6. **No Production Insecure Flag**: The `--insecure` flag should only be used in isolated development environments. In production, verified ownership and authenticated Stalwart SMTP are required.
