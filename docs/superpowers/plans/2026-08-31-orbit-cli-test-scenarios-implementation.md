# Orbit CLI: Comprehensive Test Scenarios & Isolated Testbed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement an isolated, containerized testbed and comprehensive automated test harness covering 100% of `orbit-cli` and `orbit-server` scenario branches (`ADM-01` through `WKS-01`) using `mcr.microsoft.com/devcontainers/base:ubuntu` and simulated service discovery without modifying the host production environment.

**Architecture:** A dual-container Docker testbed (`orbit-test-edge` and `orbit-test-client`) running on an isolated bridge network (`orbit-test-net`). The edge container runs an ephemeral `orbit-server` and mock service endpoints for `orbit-staff`, Forgejo SSH registration, and R2 asset storage. The client container runs on Ubuntu LTS with `/etc/hosts` aliases resolving `*.dev.manova.space` to the edge container, executing an automated scenario driver suite that verifies all CLI commands, state mutations, error branches, and permissions.

**Tech Stack:** Go 1.26, Docker / Docker Compose v2, `mcr.microsoft.com/devcontainers/base:ubuntu`, Bash, SQLite, HMAC-SHA256, Ed25519, Cobra.

## Global Constraints

- **Zero Host Mutation**: Do not alter host `/etc/hosts`, host ports `80`, `443`, `8080`, or existing production containers (Forgejo, LLDAP, Caddy, Postgres, Redis).
- **Network Isolation**: All testbed traffic must flow exclusively across the internal `orbit-test-net` bridge network.
- **Strict YAGNI & Hermetic Execution**: No dependency on public internet access during testbed scenario execution.

---

### Task 1: Lightweight Edge Mock Daemon

**Files:**
- Create: `orbit/orbit-cli/test/testbed/mockserver/main.go`
- Test: `orbit/orbit-cli/test/testbed/mockserver/main_test.go`

**Interfaces:**
- Produces: HTTP server listening on `:10800` (staff API with HMAC verification), `:8080` (orbit-server onboarding/challenge APIs), `:9000` (S3/R2 mock asset store), and `:3000` (Forgejo SSH key registration mock).

- [ ] **Step 1: Write mock server test**
Write `test/testbed/mockserver/main_test.go` to test challenge initiation, HMAC staff authentication verification, and mock S3 upload/download.

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./test/testbed/mockserver/...` in `orbit/orbit-cli`.
Expected: FAIL (package/files do not exist yet).

- [ ] **Step 3: Implement mock server daemon**
Implement `test/testbed/mockserver/main.go` handling:
- `POST /v1/owner/challenge` & `POST /v1/owner/verify`
- `POST /v1/onboard/claim` & `GET /v1/onboard/validate`
- `POST /v1/staff/create`, `GET /v1/staff/list`, `GET /v1/staff/:uid`, `PUT /v1/staff/:uid`, `POST /v1/staff/:uid/disable`, `POST /v1/staff/:uid/enable`, `DELETE /v1/staff/:uid`, `POST /v1/staff/:uid/reset-password` (using `staffhmac.VerifyRequest`)
- `/api/v1/user/keys` (Forgejo SSH mock)
- Simple S3 PutObject / GetObject / HeadObject mock on `:9000`

- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./test/testbed/mockserver/...` in `orbit/orbit-cli`.
Expected: PASS.

---

### Task 2: Container Testbed & Network Configuration

**Files:**
- Create: `orbit/orbit-cli/test/testbed/Dockerfile.testbed`
- Create: `orbit/orbit-cli/test/testbed/docker-compose.test.yml`
- Create: `orbit/orbit-cli/test/testbed/entrypoint.sh`

**Interfaces:**
- Produces: Isolated Docker network `orbit-test-net` with `orbit-test-edge` and `orbit-test-client` containers and host aliases for `orbit.dev.manova.space`, `staff.dev.manova.space`, `git.dev.manova.space`, and `assets.dev.manova.space`.

- [ ] **Step 1: Create Testbed Dockerfile**
Create `Dockerfile.testbed` based on `mcr.microsoft.com/devcontainers/base:ubuntu` installing Go 1.26, git, zsh, curl, sudo, and test tools.

- [ ] **Step 2: Create Testbed docker-compose definition**
Create `docker-compose.test.yml` configuring:
- `orbit-test-net` bridge network
- `orbit-test-edge` container running mock server and ephemeral SQLite store
- `orbit-test-client` container with `extra_hosts` pointing `*.dev.manova.space` to `orbit-test-edge`

- [ ] **Step 3: Verify container build and networking**
Run `docker compose -f test/testbed/docker-compose.test.yml build` and test container communication.

---

### Task 3: Comprehensive Scenario Test Driver

**Files:**
- Create: `orbit/orbit-cli/test/scenarios/run_all.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_admin.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_staff.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_invite.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_onboard.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_doctor.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_assets.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_port.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_env.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_config.sh`
- Create: `orbit/orbit-cli/test/scenarios/test_workspace.sh`

**Interfaces:**
- Produces: Modular scenario scripts covering test cases `ADM-01..07`, `STF-01..05`, `INV-01..03`, `ONB-01..03`, `DOC-01..02`, `AST-01..02`, `PRT-01`, `ENV-01`, `CFG-01`, `WKS-01`, asserting outputs, exit codes, and file permissions.

- [ ] **Step 1: Implement Admin & Staff Scenarios**
Write `test_admin.sh` and `test_staff.sh` covering hermetic init, remote OTP, grant creation, 3-strike burn, secret rotation, staff CRUD, HMAC rejection, and reserved account guards.

- [ ] **Step 2: Implement Invite & Onboard Scenarios**
Write `test_invite.sh` and `test_onboard.sh` covering invite token signing/inspection/revocation, 8-stage automated onboarding, session checkpoint/resume, and invalid token rejection.

- [ ] **Step 3: Implement Doctor, Assets, Port, Env, Config, Workspace Scenarios**
Write `test_doctor.sh`, `test_assets.sh`, `test_port.sh`, `test_env.sh`, `test_config.sh`, `test_workspace.sh`.

- [ ] **Step 4: Implement Master Runner `run_all.sh`**
Aggregate all scenario scripts with formatted console reporting, timing, and non-zero exit on any failure.

---

### Task 4: Host-Side One-Click Testbed Runner Script

**Files:**
- Create: `orbit/orbit-cli/scripts/test-orbit-scenarios.sh`

**Interfaces:**
- Produces: Executable `./scripts/test-orbit-scenarios.sh` that builds binaries, boots containers, executes `run_all.sh` inside the client container, gathers logs, and cleans up.

- [ ] **Step 1: Implement `test-orbit-scenarios.sh`**
Implement runner with traps for `EXIT`, `INT`, `TERM` to guarantee clean container teardown.

- [ ] **Step 2: Make executable and verify dry-run flags**
`chmod +x orbit/orbit-cli/scripts/test-orbit-scenarios.sh`

---

### Task 5: In-Tree Go Integration Test Matrix

**Files:**
- Create: `orbit/orbit-cli/cmd/orbit/scenarios_integration_test.go`

**Interfaces:**
- Produces: Hermetic Go integration tests matching all scenario IDs that run as part of standard `go test ./...`.

- [ ] **Step 1: Write `scenarios_integration_test.go`**
Add end-to-end integration tests using Go's `httptest.Server` and temporary directories covering all CLI commands.

- [ ] **Step 2: Run in-tree tests**
Run: `go test -v ./cmd/orbit -run TestScenariosIntegration`
Expected: PASS.

---

### Task 6: Full Testbed Execution & Verification

- [ ] **Step 1: Execute Full Isolated Container Scenario Run**
Run: `./scripts/test-orbit-scenarios.sh` in `orbit/orbit-cli`.
Expected: All 23 scenarios execute inside the container testbed and return PASS.

- [ ] **Step 2: Verify Host State Preservation**
Confirm that host Docker containers, `/etc/hosts`, and git status remain clean and untouched.
