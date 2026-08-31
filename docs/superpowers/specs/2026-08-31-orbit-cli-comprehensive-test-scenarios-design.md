# Orbit CLI: Comprehensive Test Scenarios & Isolated Testbed Specification

**Document Version:** 1.0.0  
**Date:** 2026-08-31  
**Status:** Approved  
**Author:** Orbit Platform Team  

---

## 1. Executive Summary & Objectives

This specification defines the exhaustive test scenario matrix, isolation architecture, service discovery mocking, and automated execution harness for the **Orbit Developer CLI** (`orbit-cli` and `orbit-server`).

### Core Objectives
1. **100% Branch & Scenario Coverage**: Exercise every command group (`admin`, `staff`, `invite`, `onboard`, `doctor`, `assets`, `port`, `env`, `config`, `sync`, `status`, `repair`, `migrate`, `dev`), validating both happy paths and failure/error states (e.g. 3-strike grant incinerations, HMAC tampering, reserved accounts, unverified vaults, broken network fallbacks).
2. **Zero-Impact Host Isolation**: Execute full client lifecycle tests inside an ephemeral Docker container (`mcr.microsoft.com/devcontainers/base:ubuntu`) over an isolated bridge network, leaving the host system and running production services completely untouched.
3. **High-Fidelity Service Discovery & Networking**: Simulate the production domain topology (`orbit.dev.manova.space`, `staff.dev.manova.space`, `git.dev.manova.space`, `assets.dev.manova.space`) using container-internal `/etc/hosts` mappings and dedicated mock services.
4. **Dual Testing Tier**:
   - **Tier 1 (Go Integration Tests)**: Hermetic, fast in-tree Go tests for CI/CD.
   - **Tier 2 (E2E Scenario Testbed)**: Automated container runner script (`scripts/test-orbit-scenarios.sh`) verifying real workstation filesystem, permissions, and network behavior.

---

## 2. Network & Testbed Architecture

### 2.1 Topology Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       HOST MACHINE (Production / Unaltered)                 │
│                                                                             │
│   Production Services (Forgejo, LLDAP, Caddy, Postgres, Redis) ── [Protected]│
│                                                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │            Docker Bridge Network: `orbit-test-net` (Isolated)       │   │
│   │                                                                     │   │
│   │  ┌────────────────────────┐         ┌────────────────────────────┐  │   │
│   │  │   Testbed Mock/Edge    │         │      Client Testbed        │  │   │
│   │  │       Container        │◄────────┤        Container           │  │   │
│   │  │  (orbit-test-edge)     │ HTTP    │  (orbit-test-client)       │  │   │
│   │  │                        │ DNS /   │ (mcr.microsoft.com/        │  │   │
│   │  │ - orbit-server (:8080) │ /etc/   │  devcontainers/base:ubuntu)│  │   │
│   │  │ - orbit-staff mock     │ hosts   │ - Clean non-root user      │  │   │
│   │  │   (:10800)             │         │ - Clean ~/.config/orbit/   │  │   │
│   │  │ - Ephemeral SQLite DB  │         │ - Clean ~/.ssh/            │  │   │
│   │  │ - S3/R2 mock (:9000)   │         │ - Compiled `orbit` binary  │  │   │
│   │  │ - Mail/SMTP mock       │         │ - Scenario Driver Runner   │  │   │
│   │  └────────────────────────┘         └────────────────────────────┘  │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Network & DNS Isolation Strategy
1. **Docker Network**: `orbit-test-net` (driver: bridge).
2. **Container Aliases & `/etc/hosts` Routing**:
   - `orbit.dev.manova.space` → `orbit-test-edge`
   - `staff.dev.manova.space` → `orbit-test-edge`
   - `git.dev.manova.space` → `orbit-test-edge`
   - `assets.dev.manova.space` → `orbit-test-edge`
3. **No Host Port Publishing**: Edge and client communicate strictly over the internal container network bridge without binding host ports `80`, `443`, `8080`, or `10800`.

---

## 3. Comprehensive Scenario Matrix & Branch Coverage

### 3.1 Platform Ownership & Admin (`orbit admin`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `ADM-01` | **Hermetic Admin Init** | Clean `~/.config/orbit` | `orbit admin init admin@manova.space --no-send --code 123456` | Exit code 0; `~/.config/orbit/owner.json` created with mode `0600`; status displays verified; 32-byte secret generated. |
| `ADM-02` | **Remote Server OTP Init** | `orbit-server` active | `orbit admin init admin@manova.space --server http://orbit.dev.manova.space:8080 --code <otp>` | Requests OTP challenge from edge server; validates challenge; seals local owner vault. |
| `ADM-03` | **Idempotent Init & Force Override** | Vault already verified | 1. Run `orbit admin init admin@manova.space`<br>2. Run `orbit admin init admin@manova.space --force --no-send --code 654321` | 1. Exits cleanly with "already verified" message, vault unchanged.<br>2. Regenerates root key, updates fingerprint, saves new timestamp. |
| `ADM-04` | **8-Digit CSPRNG Grant Creation** | Verified owner vault | `orbit admin grant --email dev@manova.space --ttl 10m --role admin` | Outputs 8-digit numeric code; saves salted SHA-256 hash in SQLite store; dispatches formatted alert to Telegram Secrets topic. |
| `ADM-05` | **3-Strike Grant Attempt Burn** | Active 8-digit grant | Submit 3 consecutive invalid grant codes to `orbit-server` verify endpoint | First 2 invalid attempts return 400 with remaining attempts counter; 3rd invalid attempt permanently incinerates grant; subsequent attempt with correct code returns 404/410 burned. |
| `ADM-06` | **Vault Secret Rotation** | Verified owner vault | `orbit admin rotate-secret` | Creates backup `owner.json.bak-<timestamp>`; generates new root secret; updates fingerprint; validates new key can sign invites. |
| `ADM-07` | **TOTP Enrollment & QR Render** | Active user | `orbit admin totp --email dev@manova.space` | Generates Authelia-compatible `otpauth://` URI and ASCII QR code. |

---

### 3.2 Staff Control Plane (`orbit staff`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `STF-01` | **Staff Create with Invite & TOTP** | Verified owner vault; `orbit-staff` live | `orbit staff create --uid alice --name "Alice" --forward alice@example.com --totp --invite` | Creates LLDAP user, Stalwart mailbox, generates Authelia TOTP secret, issues onboarding invite token; prints passwords + `otpauth://` + invite token. |
| `STF-02` | **HMAC Authentication & Tamper Rejection** | Unmatched HMAC secret | `orbit staff list --server http://staff.dev.manova.space:10800` (with invalid secret in vault) | Rejects request with HTTP 401 `{"error":"bad hmac"}`. |
| `STF-03` | **Reserved Account Guard** | Verified owner vault | `orbit staff create --uid admin --forward admin@example.com` | Rejects with HTTP 403 / validation error for reserved names (`admin`, `authelia-bind`, `verdaccio-bind`, `verdaccio-ci`). |
| `STF-04` | **Staff Lifecycle (Get, Update, Disable, Enable, Delete)** | Existing user `alice` | 1. `orbit staff get alice`<br>2. `orbit staff update alice --forward new@example.com`<br>3. `orbit staff disable alice`<br>4. `orbit staff enable alice`<br>5. `orbit staff delete alice` | 1. Returns user details.<br>2. Forward email updated.<br>3. Status changes to disabled; mailbox password rotated.<br>4. Status changes to active.<br>5. LLDAP user, mailbox, and TOTP purged. |
| `STF-05` | **Atomic Recreate & Password Resets** | Existing user `bob` | 1. `orbit staff reset-password bob --ldap --totp`<br>2. `orbit staff recreate --uid bob --name "Bob" --forward bob@example.com --totp` | 1. Rotates SSO password and regenerates TOTP without mailbox change.<br>2. Atomically wipes and recreates user from scratch. |

---

### 3.3 Invitation Engine (`orbit invite`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `INV-01` | **Invite Creation & Claims Inspection** | Verified owner vault | 1. `orbit invite create --email charlie@example.com --name "Charlie" --scope core --ttl 7d`<br>2. `orbit invite inspect <token>` | 1. Returns signed HMAC-SHA256 token.<br>2. Decodes claims: ID, email, name, scope (`core`), expiration timestamp; confirms signature valid. |
| `INV-02` | **Invite Token Revocation** | Existing invite token | 1. `orbit invite revoke <invite-id>`<br>2. `orbit invite list`<br>3. Attempt claim via `POST /v1/onboard/claim` | 1. Revocation flag set.<br>2. `list` shows status `revoked`.<br>3. Claim rejected with 401/403 "token revoked". |
| `INV-03` | **Tampered / Expired Token Rejection** | Modified token string or expired TTL | `orbit invite inspect <corrupted-token>` | Fails signature verification with error "invalid signature". |

---

### 3.4 Zero-Touch Onboarding Pipeline (`orbit onboard`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `ONB-01` | **8-Stage Automated Pipeline** | Clean client machine; valid onboarding token | `orbit onboard --token <token> --server http://orbit.dev.manova.space:8080 --non-interactive` | 1. Stage 1 (Doctor): Verifies environment.<br>2. Stage 2 (SSH Keypair): Generates `~/.ssh/id_ed25519_orbit`.<br>3. Stage 3 (Claim): Submits SSH public key, registers with Forgejo mock.<br>4. Stage 4 (Network): Validates DNS resolution & TLS setup.<br>5. Stage 5 (Clone): Clones workspace repositories defined in scope.<br>6. Stage 6 (Assets): Pulls media assets from R2 mock.<br>7. Stage 7 (Toolchain & MCP): Configures `.cursor/mcp.env` (mode 0600) and `.zshrc`.<br>8. Stage 8 (Completion): Prints summary and 2FA QR code. |
| `ONB-02` | **Session Checkpoint & Resumption** | Onboarding interrupted after Stage 3 | 1. Run onboarding until Stage 3 completes, simulate abort.<br>2. Run `orbit onboard --resume --non-interactive` | Resumes from Stage 4 without re-generating SSH key or re-claiming token. |
| `ONB-03` | **Invalid Token Onboarding Attempt** | Expired or forged token | `orbit onboard --token invalid-token-str --non-interactive` | Fails gracefully at Stage 3 with descriptive error message; rollback cleans up temporary state. |

---

### 3.5 System Diagnostics & Auto-Healing (`orbit doctor`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `DOC-01` | **Doctor Pre-Flight Checks** | Unconfigured workspace | `orbit doctor` | Detects missing directories, unconfigured `.cursor/mcp.env`, missing git hooks; reports actionable warning list. |
| `DOC-02` | **Doctor Auto-Heal (`--fix`)** | Unconfigured workspace | `orbit doctor --fix` | Runs healer recipes: creates `manovaspace`, `clients`, `documents`, `share`, `temp`; configures `.githooks`; initializes `.cursor/mcp.env` (0600); verifies all checks turn green. |

---

### 3.6 Media Assets Synchronization (`orbit assets`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `AST-01` | **Asset Pull & Verification** | Mock R2 S3 store active | 1. `orbit assets status`<br>2. `orbit assets pull`<br>3. `orbit assets verify` | 1. Reports missing local assets according to `orbit-assets.yaml`.<br>2. Downloads assets from mock S3 store.<br>3. Verifies SHA-256 checksums match manifest. |
| `AST-02` | **Asset Diff & Push** | Local file modified | 1. Modify local asset file.<br>2. `orbit assets diff`<br>3. `orbit assets push`<br>4. `orbit assets lock` | 1. Diff shows size and checksum mismatch.<br>2. Uploads new asset to R2 store.<br>3. Updates `orbit-assets.yaml` checksum and lockfile. |

---

### 3.7 Port Block Allocation (`orbit port` - ADR-006)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `PRT-01` | **Port Allocation & Conflict Detection** | Standard port registry | 1. `orbit port list`<br>2. `orbit port check 10422`<br>3. `orbit port allocate --service my-svc --range dev`<br>4. `orbit port free my-svc` | 1. Lists active 50-port blocks.<br>2. Confirms port 10422 is allocated to NATS.<br>3. Allocates next available 50-port slice.<br>4. Frees allocated port block cleanly. |

---

### 3.8 Environment Contracts (`orbit env`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `ENV-01` | **Environment Contract Validation** | Directory with `.env.example` | 1. `orbit env schema`<br>2. `orbit env check` with incomplete `.env`<br>3. `orbit env sample` | 1. Generates JSON schema from `.env.example`.<br>2. Fails with list of missing required variables.<br>3. Generates populated `.env.sample`. |

---

### 3.9 Configuration Hardening & Secret Masking (`orbit config`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `CFG-01` | **Secret Masking & Atomic Writes** | Fresh configuration | 1. `orbit config init`<br>2. `orbit config set server.smtp_pass "super-secret-password"`<br>3. `orbit config show`<br>4. `orbit config get server.smtp_pass` | 1. Creates `~/.config/orbit/config.yaml` with mode `0600`.<br>2. Saves value atomically.<br>3. `config show` prints `smtp_pass: "********"`.<br>4. `config get` explicitly returns plaintext when requested. |

---

### 3.10 Workspace Orchestration (`orbit sync`, `status`, `repair`, `init`)

| ID | Scenario | Preconditions | Execution Steps | Expected Assertions & State Checks |
|---|---|---|---|---|
| `WKS-01` | **Multi-Repo Workspace Lifecycle** | Mock Git server with multi-repo manifest | 1. `orbit init core`<br>2. `orbit status`<br>3. Create dirty commit in a repo.<br>4. `orbit sync`<br>5. `orbit repair` | 1. Clones repositories defined under `core` scope.<br>2. Reports clean working trees.<br>3. `status` flags modified/untracked files.<br>4. `sync` fast-forwards clean repos.<br>5. `repair` attaches `.git` to gitless repos cleanly. |

---

## 4. Testbed Components & Implementation Plan

### 4.1 Deliverable Files
1. **Mock Edge Service (`test/testbed/mockserver/main.go`)**:
   - Implements endpoints:
     - `POST /v1/owner/challenge` & `POST /v1/owner/verify`
     - `POST /v1/onboard/claim` & `GET /v1/onboard/validate`
     - `POST /v1/staff/create`, `GET /v1/staff/list`, `DELETE /v1/staff/:uid`, etc. (with HMAC verification)
     - S3-compatible mock endpoints for `orbit assets`
2. **Testbed Dockerfile (`test/testbed/Dockerfile.testbed`)**:
   - Based on `mcr.microsoft.com/devcontainers/base:ubuntu`.
   - Installs Go 1.26 toolchain, Git, zsh, sudo, and test dependencies.
3. **Scenario Runner (`test/scenarios/run_all.sh` & `scripts/test-orbit-scenarios.sh`)**:
   - Builds `orbit` and `orbit-server` binaries.
   - Bootstraps the Docker test network and containers.
   - Executes all test scenarios sequentially with colorized pass/fail output.
   - Cleans up containers and networks automatically on completion or interrupt.
4. **In-Tree Go Integration Tests**:
   - Comprehensive unit and integration tests across `pkg/` and `cmd/orbit` matching all scenario IDs.

---

## 5. Success Criteria & Verification Proof

- **All Scenarios Pass (`23/23`)**: Every scenario in Section 3 succeeds with exit code 0.
- **Zero Host Mutation**: No files created outside `orbit/orbit-cli` repository; host Docker containers and networks remain completely unaltered.
- **100% Hermetic Execution**: The entire testbed can run repeatedly and idempotently both locally and in CI/CD without network access to the live internet.
