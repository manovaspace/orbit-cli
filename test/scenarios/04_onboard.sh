#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 04: Zero-Touch Onboarding Pipeline (ONB-01 through ONB-03)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built
ensure_mock_server_running

# ------------------------------------------------------------------------------
# ONB-01: Happy Path Automated Onboarding Pipeline
# ------------------------------------------------------------------------------
scenario_start "ONB-01" "Automated 8-Stage Onboarding Claim Pipeline"
setup_test_sandbox "onb-01"
seed_owner_vault

log_step "Generating valid onboarding invite token..."
run_orbit invite create developer@manova.space --name "Dev One" --scope "core" --no-send >/dev/null
invite_json=$(run_orbit invite list --format json)
token=$(echo "${invite_json}" | jq -r '.[0].token')

log_step "Running non-interactive onboarding pipeline..."
onboard_out=$(run_orbit onboard \
    --token "${token}" \
    --server "${ORBIT_API_URL}" \
    --workspace "${SANDBOX_DIR}/workspace" \
    --non-interactive \
    --skip-stack \
    --auto-fix)

assert_contains "${onboard_out}" "ORBIT DEVELOPER WIZARD" "Wizard banner displayed"
assert_file_exists "${HOME}/.ssh/id_ed25519" "Ed25519 SSH private key generated"
assert_file_exists "${HOME}/.ssh/id_ed25519.pub" "Ed25519 SSH public key generated"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ONB-02: Diagnostic Bundle Generation & Checkpoint Reset
# ------------------------------------------------------------------------------
scenario_start "ONB-02" "Diagnostic Bundle Generation & Checkpoint Reset"
setup_test_sandbox "onb-02"
seed_owner_vault

log_step "Generating diagnostic bundle..."
bundle_file="${SANDBOX_DIR}/diag-bundle.tar.gz"
diag_out=$(run_orbit onboard --diag-bundle "${bundle_file}")
assert_contains "${diag_out}" "Orbit Diagnostic Bundle Generated" "Diagnostic bundle output confirms creation"
assert_file_exists "${bundle_file}" "Diagnostic tar.gz archive created"

log_step "Testing checkpoint reset flag (--reset)..."
reset_out=$(run_orbit onboard --reset)
assert_contains "${reset_out}" "session cleared" "Reset clears session checkpoint"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ONB-03: Invalid Token Rejection & Dry-Run
# ------------------------------------------------------------------------------
scenario_start "ONB-03" "Invalid Token Rejection & Dry-Run Preview"
setup_test_sandbox "onb-03"
seed_owner_vault

log_step "Executing dry-run pre-flight check..."
dry_out=$(run_orbit onboard --dry-run)
assert_contains "${dry_out}" "DRY-RUN PREVIEW" "Dry-run executes preview"
assert_contains "${dry_out}" "Pre-Flight" "Dry-run executes pre-flight diagnostics"

log_step "Resetting session before invalid token claim..."
run_orbit onboard --reset >/dev/null 2>&1 || true

log_step "Attempting onboarding with invalid token string..."
set +e
invalid_out=$(run_orbit onboard --token "invalid-bad-token-string" --server "${ORBIT_API_URL}" --non-interactive --skip-stack 2>&1)
set -e
assert_contains "${invalid_out}" "invalid" "Invalid token onboarding fails gracefully with error"

cleanup_test_sandbox
scenario_end

print_scenario_summary "04_onboard.sh"
