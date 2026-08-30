# Design Specification: `orbit-server` Modernization & Architecture Redesign

- **Date**: 2026-08-30
- **Status**: Approved
- **Scope**: `orbit/orbit-cli` (`cmd/orbit-server`, `pkg/serverstore`, `pkg/onboard`, `pkg/owner`, `pkg/invite`)

---

## 1. Problem Statement & Motivation

`orbit-server` is the edge daemon in the Orbit dual-binary platform. While operational for basic flows, four architectural shortcomings limit its reliability, security, and maintainability:

1. **Non-Atomic File I/O & Concurrency Risk**: Invites (`~/.config/manova/invites.json`) and owner records (`~/.config/orbit/owner.json`) are written via raw `os.WriteFile()`, which truncates files directly. Concurrent claims or ungraceful daemon terminations risk corrupting JSON files.
2. **Volatile In-Memory Verification State**: Admin OTP challenges (`pkg/owner/challenge.go`) and admin grants are stored in memory. Daemon restarts or container redeployments wipe active challenges, breaking in-progress verification.
3. **Rate Limiting & Proxy Vulnerabilities**: The sliding-window rate limiter trusts raw `X-Forwarded-For` without upstream proxy validation, enabling IP spoofing. Additionally, it lacks per-email attempt lockouts to defend against distributed OTP brute-force attacks.
4. **Unstructured Telemetry**: Daemon request handling lacks structured `log/slog` JSON logging, correlation request IDs (`X-Request-ID`), and OpenTelemetry tracing compatible with `orbit-observability`.

---

## 2. Architecture & Design

### 2.1 Package Layout

```
orbit/orbit-cli/
├── cmd/
│   └── orbit-server/
│       └── main.go                     # Daemon entrypoint, flag parsing, graceful shutdown
└── pkg/
    ├── serverstore/                    # Unified persistence layer
    │   ├── store.go                    # Store, InvitesStore, ChallengeStore, GrantStore interfaces
    │   ├── sqlite/
    │   │   ├── db.go                   # modernc.org/sqlite connection pool, WAL mode, migrations
    │   │   ├── schema.go               # DDL migration scripts (v1)
    │   │   ├── invites.go              # Invite CRUD, lookup by ID, prefix, and token
    │   │   ├── challenges.go           # OTP challenge lifecycle (create, verify, attempt tracking)
    │   │   ├── grants.go               # 8-digit admin grants repository
    │   │   ├── ratelimit.go            # Persistent rate-limit sliding window storage
    │   │   └── sqlite_test.go          # In-memory (:memory:) integration tests
    │   └── migrate_json.go             # Auto-migration from legacy JSON files on first boot
    ├── onboard/
    │   ├── server.go                   # HTTP handlers, claim endpoints, health probes
    │   ├── middleware/
    │   │   ├── logging.go              # slog request logger with latency & status codes
    │   │   ├── requestid.go            # X-Request-ID injection
    │   │   └── recovery.go             # Panic recovery with structured error logging
    │   └── ratelimit/
    │       └── limiter.go              # Hardened limiter with proxy CIDR verification & email lockouts
    └── owner/
        ├── challenge.go                # High-level challenge manager backed by serverstore.Store
        └── store.go                    # Owner vault repository with atomic writes
```

---

## 3. Detailed Data Model & Database Schema

The persistent SQLite database resides at `~/.config/orbit/orbit.db` (overrideable via `ORBIT_DB_PATH` or `--db-path`).

```sql
-- Migration 001_initial_orbit_server.sql

PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

-- 1. Onboarding Invitations
CREATE TABLE IF NOT EXISTS invites (
    id TEXT PRIMARY KEY,
    token TEXT UNIQUE NOT NULL,
    email TEXT NOT NULL,
    display_name TEXT,
    scope TEXT NOT NULL,
    created_by TEXT,
    revoked BOOLEAN NOT NULL DEFAULT 0,
    revoked_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    metadata_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_invites_email ON invites(email);
CREATE INDEX IF NOT EXISTS idx_invites_token ON invites(token);

-- 2. Out-of-Band OTP Challenges
CREATE TABLE IF NOT EXISTS challenges (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    otp_hash TEXT NOT NULL,
    salt TEXT NOT NULL,
    attempts_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    verified BOOLEAN NOT NULL DEFAULT 0,
    verified_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_challenges_email ON challenges(email);
CREATE INDEX IF NOT EXISTS idx_challenges_expires ON challenges(expires_at);

-- 3. Administrator Grants (8-digit single-use codes)
CREATE TABLE IF NOT EXISTS admin_grants (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    salt TEXT NOT NULL,
    role TEXT NOT NULL,
    created_by TEXT NOT NULL,
    used BOOLEAN NOT NULL DEFAULT 0,
    used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_grants_email ON admin_grants(email);

-- 4. Rate Limiting Events
CREATE TABLE IF NOT EXISTS rate_limit_events (
    key TEXT NOT NULL,
    route TEXT NOT NULL,
    timestamp INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rate_limit_key_ts ON rate_limit_events(key, timestamp);

-- 5. Schema Versions
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL
);
```

---

## 4. Security & Reverse Proxy Hardening

### 4.1 Trusted Proxy Header Validation
When determining client IP:
1. Parse incoming `r.RemoteAddr` (host part).
2. Check if `RemoteAddr` matches configured `TrustedProxies` CIDRs (default: `127.0.0.1/32`, `::1/128`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`).
3. If trusted, extract the rightmost non-trusted IP from `X-Forwarded-For`.
4. If not from a trusted proxy, ignore `X-Forwarded-For` and use `RemoteAddr` directly.

### 4.2 Multi-Layer Rate Limiting
- **Layer 1: IP Sliding Window**: Maximum 10 requests per minute per IP on `/v1/onboard/claim` and `/api/v1/admin/challenge`. Returns `429 Too Many Requests` with `Retry-After` header.
- **Layer 2: Challenge Lockout**: If `attempts_count >= max_attempts` (3 attempts), the challenge is permanently marked invalid.
- **Layer 3: Email Flood Protection**: Maximum 5 challenge requests per hour per email address to prevent SMTP resource exhaustion.

---

## 5. Observability, Logging & Telemetry

### 5.1 Structured Logging (`log/slog`)
`orbit-server` initializes a JSON `slog.Logger` in server mode and registers HTTP middleware:
- Assigns or propagates `X-Request-ID`.
- Logs request start and completion with `method`, `path`, `status`, `duration_ms`, `client_ip`, `request_id`, and `user_agent`.
- Logs failures with structured error attributes.

### 5.2 OpenTelemetry
- Wraps the root HTTP handler with `otelhttp.NewHandler(mux, "orbit-server")`.
- Automatically extracts incoming W3C `traceparent` headers, recording spans for claim negotiation and verification requests.

---

## 6. Migration & Backward Compatibility

1. **Pure-Go Driver**: Uses `modernc.org/sqlite`, avoiding any CGO dependencies or external SQLite shared libraries.
2. **Automatic Legacy Import**:
   - On startup, if SQLite `invites` table is empty, checks for legacy `~/.config/manova/invites.json` and `~/.config/orbit/invites.json`.
   - Imports all existing invitations transactionally into SQLite and renames the legacy file to `.bak`.
3. **Atomic Writes for Keyring/Owner**:
   - `pkg/owner/store.go` adopts `writeTempAndRename()` with `0600` permissions.

---

## 7. Verification Plan

1. **Unit & Integration Tests**:
   - `pkg/serverstore/sqlite`: Test concurrent invite insertions, duplicate token rejections, OTP challenge verification, attempts increment, and automatic expiration pruning using `:memory:`.
   - `pkg/onboard/middleware`: Test `X-Request-ID` generation, structured logger outputs, and panic recovery.
   - `pkg/onboard/ratelimit`: Test trusted vs untrusted `X-Forwarded-For` spoofing, window reset, and email rate limiting.
2. **End-to-End Workflow Verification**:
   - `orbit invite create test@manova.space` $\to$ persists in SQLite.
   - `orbit onboard --token <token>` $\to$ claims token from `orbit-server`, records idempotency, and provisions account.
   - `orbit admin challenge` + `orbit admin verify` $\to$ verifies challenge surviving server restart.
