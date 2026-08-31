# Implementation Plan: List Pagination, Installer Prereqs, TOTP QR Codes, & Email Dispatch

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement pagination for all CLI table lists, auto-installation of missing prerequisites with user confirmation in `install.sh`, complete TOTP QR code integration for all invite links in the web UI, and email dispatch on staff creation.

**Architecture:**
1. Extend `pkg/table` with pagination slicing (`WithPagination(page, limit int)`) and footer info rendering; wire `--page` and `--limit` flags to `staff list`, `invite list`, `config list`, `port list`, `migrate status`, and `status`.
2. Update `install.sh` and `pkg/onboard/install.sh` to check for missing packages (`zsh`, `curl`, `chsh`, `PATH`) and prompt for interactive user confirmation before auto-installing via `sudo apt-get` (auto-approved on `-y` / `--yes`).
3. Wire `--totp` generation in `orbit invite create` and ensure `orbit staff create --invite` embeds `metadata["otpauth"]` in the signed token so the web UI setup portal automatically renders the 2FA SVG QR code.
4. Wire SMTP email dispatch in `cmd/orbit/staff.go` (`create` and `recreate` with `--invite`) and enhance `pkg/invite/mailer.go` to fall back to `/etc/orbit/orbit-server.env`.

**Tech Stack:** Go 1.26, Cobra, Lipgloss, Bash, HTML/CSS/JS (embedded in Go).

---

### Task 1: Table Engine Pagination & CLI List Commands

**Files:**
- Modify: `orbit/orbit-cli/pkg/table/table.go`
- Modify: `orbit/orbit-cli/pkg/table/render.go`
- Modify: `orbit/orbit-cli/pkg/table/table_test.go`
- Modify: `orbit/orbit-cli/cmd/orbit/staff.go`
- Modify: `orbit/orbit-cli/cmd/orbit/invite.go`
- Modify: `orbit/orbit-cli/cmd/orbit/config.go`
- Modify: `orbit/orbit-cli/cmd/orbit/port.go`
- Modify: `orbit/orbit-cli/cmd/orbit/migrate.go`
- Modify: `orbit/orbit-cli/cmd/orbit/status.go`
- Modify: `orbit/orbit-cli/cmd/orbit/staff_test.go`
- Modify: `orbit/orbit-cli/cmd/orbit/invite_test.go`

**Interfaces:**
- `Table.WithPagination(page int, limit int) *Table`
- CLI flags: `--page int` (default 1), `--limit int` (default 0 = unpaginated or positive integer)

- [x] **Step 1: Write unit tests in `pkg/table/table_test.go` for pagination**
  - Test `WithPagination(1, 2)` on 5 rows returns first 2 rows and footer: `Showing 1-2 of 5 rows (Page 1/3)`.
  - Test `WithPagination(3, 2)` returns row 5: `Showing 5-5 of 5 rows (Page 3/3)`.
  - Test out of bounds `WithPagination(4, 2)` returns empty rows with pagination message.
  - Test unpaginated `WithPagination(0, 0)` renders all rows without footer.

- [x] **Step 2: Implement pagination in `pkg/table/table.go` and `pkg/table/render.go`**
  - Add `page int` and `limit int` fields to `Table` struct.
  - In `Render(w io.Writer)`, slice `t.rows` based on `page` and `limit`.
  - Append pagination footer note using `subtleStyle`.

- [x] **Step 3: Add `--page` and `--limit` flags to list commands**
  - Add flags to `newStaffListCmd()`, `newInviteListCmd()`, `newConfigListCmd()`, `newPortListCmd()`, `newMigrateStatusCmd()`, and `newStatusCmd()`.
  - Pass `page` and `limit` to `tbl.WithPagination(page, limit)`.

- [x] **Step 4: Run tests and verify**
  - Run `go test ./pkg/table/... ./cmd/orbit/...`

---

### Task 2: Installer Prerequisite Auto-Healing & User Confirmation

**Files:**
- Modify: `orbit/orbit-cli/install.sh`
- Modify: `orbit/orbit-cli/pkg/onboard/install.sh`
- Test: `orbit/orbit-cli/test/scenarios/01_install.sh` or local shell tests

- [x] **Step 1: Add prerequisite detection and interactive prompt helpers in `install.sh`**
  - Check if `curl` / `wget` is installed; if missing, prompt to install via `sudo apt-get update && sudo apt-get install -y curl`.
  - Check if `zsh` is installed; if missing, prompt:
    `"  zsh is required as your login shell. Install zsh now via sudo apt-get? (y/N) [y]: "`
    and execute `sudo apt-get update -qq && sudo apt-get install -y -qq zsh`.
  - Check if login shell is `zsh` via `getent passwd`; if not, prompt:
    `"  Set zsh as your default login shell via chsh? (y/N) [y]: "`
    and execute `chsh -s "$(command -v zsh)" "$USER" || sudo chsh -s "$(command -v zsh)" "$USER"`.
  - Check if `~/.local/bin` is in `$PATH`; if not, offer to append `export PATH="$HOME/.local/bin:$PATH"` to `~/.zshrc`.
  - Ensure all prompts respect `-y` / `--yes` / `ORBIT_YES=1` for non-interactive / container environments.

- [x] **Step 2: Synchronize `pkg/onboard/install.sh`**
  - Keep `install.sh` and `pkg/onboard/install.sh` in sync.

- [x] **Step 3: Verify installer syntax and test**
  - Run `bash -n install.sh` and test with shell test scenarios.

---

### Task 3: 2FA TOTP QR Code Generation & Web UI Integration

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/invite.go`
- Modify: `orbit/orbit-cli/cmd/orbit/staff.go`
- Modify: `orbit/orbit-cli/pkg/invite/token.go`
- Modify: `orbit/orbit-cli/pkg/onboard/landing.html`
- Modify: `orbit/orbit-cli/cmd/orbit/invite_test.go`
- Modify: `orbit/orbit-cli/cmd/orbit/staff_test.go`

- [x] **Step 1: Add TOTP secret generation helper in `pkg/invite`**
  - Create helper `GenerateTOTP(issuer, account string) (secret string, uri string, err error)` using base32 random secret generation (RFC 6238 compatible).

- [x] **Step 2: Add `--totp` flag to `orbit invite create`**
  - When `--totp` is passed to `orbit invite create <email>`, generate TOTP URI and store in `claims.Metadata["otpauth"]`.
  - Print TOTP secret and QR info in terminal output.

- [x] **Step 3: Ensure `orbit staff create --invite` always handles TOTP**
  - When creating staff, ensure `--totp` flag enables TOTP enrollment on Authelia and embeds `res.OTPAuth` into `claims.Metadata["otpauth"]`.
  - Update `createStaffInvite` to format setup URLs with `https://orbit.manova.space/setup?token=...`.

- [x] **Step 4: Verify Web Portal (`landing.html`) rendering**
  - Verify that token with `metadata["otpauth"]` parses and renders the SVG QR code and secret copy box.

- [x] **Step 5: Run tests**
  - Run `go test ./pkg/invite/... ./cmd/orbit/... ./pkg/onboard/...`

---

### Task 4: Fix Invitation Email Dispatch in Staff Commands & Mailer Fallback

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/staff.go`
- Modify: `orbit/orbit-cli/pkg/invite/mailer.go`
- Modify: `orbit/orbit-cli/cmd/orbit/staff_test.go`

- [x] **Step 1: Enhance `pkg/invite/mailer.go` with config fallback**
  - In `NewMailerFromEnv()`, if `ORBIT_SMTP_PASS` is empty, check `/etc/orbit/orbit-server.env` or `~/.config/orbit/orbit-server.env` to load server SMTP credentials when available.

- [x] **Step 2: Wire email dispatch in `cmd/orbit/staff.go`**
  - In `newStaffCreateCmd()` and `newStaffRecreateCmd()`, when `--invite` is set and `--no-send` is false, initialize `mailer` and call `mailer.SendInvite(cmd.Context(), inviteEmail, emailData)`.
  - Add `--no-send` flag to `staff create` and `staff recreate`.
  - Print success/warning message with destination address and SMTP gateway.

- [x] **Step 3: Write tests in `staff_test.go`**
  - Test `staff create --invite` dispatches email to mock server or records email dispatch attempt.
  - Test `staff create --invite --no-send` suppresses email dispatch.

- [x] **Step 4: Recompile and deploy `orbit-server` & `orbit` CLI**
  - Build `orbit` and `orbit-server`.
  - Install to `/usr/local/bin` and restart `orbit-server.service`.
  - Verify with end-to-end command.
