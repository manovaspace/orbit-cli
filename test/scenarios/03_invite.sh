#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 03: Invitation Engine (INV-01 through INV-03)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built
ensure_mock_server_running

# ------------------------------------------------------------------------------
# INV-01: Generate Invite Token & JSON Output
# ------------------------------------------------------------------------------
scenario_start "INV-01" "Invite Token Generation & Claims Listing"
setup_test_sandbox "inv-01"
seed_owner_vault

log_step "Generating developer onboarding invitation for charlie..."
create_out=$(run_orbit invite create charlie@example.com \
    --name "Charlie Developer" \
    --scope "core" \
    --expires "7d" \
    --no-send)

assert_contains "${create_out}" "Developer Invitation Generated" "Invite generation title displayed"
assert_contains "${create_out}" "charlie@example.com" "Recipient email in output"
assert_contains "${create_out}" "core" "Scope core recorded"

log_step "Listing invitations in table format..."
list_table=$(run_orbit invite list)
assert_contains "${list_table}" "charlie@example.com" "Invite list contains charlie"
assert_contains "${list_table}" "active" "Status is active"

log_step "Listing invitations in JSON format..."
list_json=$(run_orbit invite list --format json)
assert_json_field "${list_json}" ".[0].email" "charlie@example.com" "JSON invite record has matching email"
assert_json_field "${list_json}" ".[0].scope" "core" "JSON invite scope matches"
assert_json_field "${list_json}" ".[0].revoked" "false" "JSON invite revoked flag is false"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# INV-02: Validate Token & Token Revocation
# ------------------------------------------------------------------------------
scenario_start "INV-02" "Token Validation & Explicit Revocation"
setup_test_sandbox "inv-02"
seed_owner_vault

log_step "Creating invitation token for revoke testing..."
run_orbit invite create revoke-me@example.com --scope "core" --no-send >/dev/null

list_json=$(run_orbit invite list --format json)
invite_id=$(echo "${list_json}" | jq -r '.[0].id')
invite_tok=$(echo "${list_json}" | jq -r '.[0].token')

log_step "Validating active token against mock server..."
val_active=$(curl -s "${ORBIT_API_URL}/v1/onboard/validate?token=${invite_tok}")
assert_json_field "${val_active}" ".valid" "true" "Active token validates as valid=true"

log_step "Revoking invitation by ID (${invite_id})..."
revoke_out=$(run_orbit invite revoke "${invite_id}")
assert_contains "${revoke_out}" "Invitation Revoked" "Revoke confirmation output"

log_step "Verifying revoked status in invite list..."
revoked_list=$(run_orbit invite list --format json)
assert_json_field "${revoked_list}" ".[0].revoked" "true" "JSON revoked field updated to true"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# INV-03: Invalid & Expired Token Rejection
# ------------------------------------------------------------------------------
scenario_start "INV-03" "Tampered / Invalid Token Rejection"
setup_test_sandbox "inv-03"
seed_owner_vault

log_step "Validating malformed/corrupted token on validation endpoint..."
val_invalid=$(curl -s "${ORBIT_API_URL}/v1/onboard/validate?token=invalid-corrupted-token-xyz")
assert_json_field "${val_invalid}" ".valid" "false" "Invalid token validates as valid=false"

log_step "Attempting to revoke a non-existent token..."
set +e
revoke_bad=$(run_orbit invite revoke non-existent-token-id-12345 2>&1)
set -e
assert_contains "${revoke_bad}" "not found" "Revoking nonexistent token returns not found error"

cleanup_test_sandbox
scenario_end

print_scenario_summary "03_invite.sh"
