#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 01: Platform Ownership & Admin (ADM-01 through ADM-07)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built
ensure_mock_server_running

# ------------------------------------------------------------------------------
# ADM-01: Hermetic Admin Init
# ------------------------------------------------------------------------------
scenario_start "ADM-01" "Hermetic Admin Init (Local In-Memory OTP)"
setup_test_sandbox "adm-01"

log_step "Initializing platform owner hermetically with explicit code..."
out=$(run_orbit admin init admin@manova.space --no-send --code 123456)
assert_contains "${out}" "Platform Ownership Verified" "Admin init confirms ownership verified"
assert_contains "${out}" "admin@manova.space" "Owner email displayed in init output"

assert_file_exists "${ORBIT_OWNER_STORE}" "Vault file owner.json created"
assert_file_mode "${ORBIT_OWNER_STORE}" "600" "Vault file has secure 0600 permissions"

log_step "Checking admin status table output..."
status_out=$(run_orbit admin status)
assert_contains "${status_out}" "Platform ownership is VERIFIED" "Status displays verified state"
assert_contains "${status_out}" "admin@manova.space" "Status displays owner email"
assert_contains "${status_out}" "0600 (secure)" "Status displays 0600 secure permissions"

log_step "Checking admin status JSON output..."
status_json=$(run_orbit admin status --format json)
assert_json_field "${status_json}" ".verified" "true" "JSON verified flag is true"
assert_json_field "${status_json}" ".email" "admin@manova.space" "JSON email matches"
assert_json_field "${status_json}" ".permissions_valid" "true" "JSON permissions_valid is true"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ADM-02: Remote Server OTP Init
# ------------------------------------------------------------------------------
scenario_start "ADM-02" "Remote Server OTP Challenge & Verification"
setup_test_sandbox "adm-02"

log_step "Initializing platform owner via mock edge server challenge..."
out=$(run_orbit admin init owner@manova.space --server "${ORBIT_API_URL}" --code 123456)
assert_contains "${out}" "Platform Ownership Verified" "Remote init verifies owner"
assert_contains "${out}" "owner@manova.space" "Output includes owner email"

assert_file_exists "${ORBIT_OWNER_STORE}" "Local vault sealed after remote verification"
assert_file_mode "${ORBIT_OWNER_STORE}" "600" "Vault permissions are 0600"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ADM-03: Idempotent Init & Force Override / OTP Failure
# ------------------------------------------------------------------------------
scenario_start "ADM-03" "Idempotent Init & Force Override"
setup_test_sandbox "adm-03"

log_step "Initial hermetic verification..."
run_orbit admin init dev-admin@manova.space --no-send --code 123456 >/dev/null

initial_status=$(run_orbit admin status --format json)
initial_fp=$(echo "${initial_status}" | jq -r .key_fingerprint)

log_step "Running idempotent re-init without --force..."
reinit_out=$(run_orbit admin init dev-admin@manova.space)
assert_contains "${reinit_out}" "already verified" "Re-init outputs already verified message"

post_status=$(run_orbit admin status --format json)
post_fp=$(echo "${post_status}" | jq -r .key_fingerprint)
assert_equals "${initial_fp}" "${post_fp}" "Key fingerprint unchanged after idempotent init"

log_step "Running force re-init with --force..."
force_out=$(run_orbit admin init dev-admin@manova.space --force --no-send --code 654321)
assert_contains "${force_out}" "Platform Ownership Verified" "Force re-init successfully executed"

force_status=$(run_orbit admin status --format json)
force_fp=$(echo "${force_status}" | jq -r .key_fingerprint)
assert_not_equals "${initial_fp}" "${force_fp}" "Key fingerprint regenerated after force init"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ADM-04: Admin Grant Create & List
# ------------------------------------------------------------------------------
scenario_start "ADM-04" "8-Digit CSPRNG Admin Grant Creation & Listing"
setup_test_sandbox "adm-04"

# Seed owner vault
run_orbit admin init admin@manova.space --no-send --code 123456 >/dev/null

log_step "Generating admin grant for delegate admin..."
grant_json=$(run_orbit admin grant delegate@manova.space --server "${ORBIT_API_URL}" --role admin --ttl 15m --json)
assert_json_field "${grant_json}" ".status" "grant_generated" "Grant generation status ok"
assert_json_field "${grant_json}" ".email" "delegate@manova.space" "Grant recipient email matches"
assert_json_field "${grant_json}" ".role" "admin" "Grant role matches"

grant_code=$(echo "${grant_json}" | jq -r .code)
log_info "Generated Grant Code: ${grant_code}"
assert_contains "${grant_code}" "-" "Grant code is formatted with dash (e.g. 1234-5678)"

log_step "Verifying grant registration on edge server..."
grant_list=$(curl -s "${ORBIT_API_URL}/api/v1/admin/grants")
assert_contains "${grant_list}" "delegate@manova.space" "Grant appears in active server grants list"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ADM-05: Admin Grant 3-Strike Burn Lockout
# ------------------------------------------------------------------------------
scenario_start "ADM-05" "Admin Grant 3-Strike Lockout & Incineration"
setup_test_sandbox "adm-05"

run_orbit admin init admin@manova.space --no-send --code 123456 >/dev/null

log_step "Creating dedicated grant for 3-strike burn test..."
grant_json=$(run_orbit admin grant burn-target@manova.space --server "${ORBIT_API_URL}" --role admin --code 8888-9999 --json)
grant_code=$(echo "${grant_json}" | jq -r .code)

log_step "Attempt 1: Invalid grant code (expect 2 remaining attempts)..."
attempt1=$(curl -s -X POST "${ORBIT_API_URL}/v1/owner/verify" -H "Content-Type: application/json" \
    -d '{"email":"burn-target@manova.space","code":"00000000"}')
assert_json_field "${attempt1}" ".remaining_attempts" "2" "Strike 1 leaves 2 remaining attempts"

log_step "Attempt 2: Invalid grant code (expect 1 remaining attempt)..."
attempt2=$(curl -s -X POST "${ORBIT_API_URL}/v1/owner/verify" -H "Content-Type: application/json" \
    -d '{"email":"burn-target@manova.space","code":"00000000"}')
assert_json_field "${attempt2}" ".remaining_attempts" "1" "Strike 2 leaves 1 remaining attempt"

log_step "Attempt 3: Invalid grant code (expect 0 remaining attempts & burn)..."
attempt3=$(curl -s -X POST "${ORBIT_API_URL}/v1/owner/verify" -H "Content-Type: application/json" \
    -d '{"email":"burn-target@manova.space","code":"00000000"}')
assert_json_field "${attempt3}" ".remaining_attempts" "0" "Strike 3 incinerates grant"
assert_contains "${attempt3}" "grant burned" "Response confirms grant is burned"

log_step "Attempt 4: Correct code submitted after burn (must be rejected)..."
attempt4=$(curl -s -X POST "${ORBIT_API_URL}/v1/owner/verify" -H "Content-Type: application/json" \
    -d '{"email":"burn-target@manova.space","code":"88889999"}')
assert_contains "${attempt4}" "burned" "Correct code rejected on burned grant"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ADM-06: Master Secret Rotation
# ------------------------------------------------------------------------------
scenario_start "ADM-06" "Master Secret Vault Key Rotation"
setup_test_sandbox "adm-06"

run_orbit admin init admin@manova.space --no-send --code 123456 >/dev/null
fp_before=$(run_orbit admin status --format json | jq -r .key_fingerprint)

log_step "Rotating master signing secret with --yes flag..."
rotate_out=$(run_orbit admin rotate-secret --yes)
assert_contains "${rotate_out}" "Master Signing Secret Rotated" "Rotation confirmation displayed"

fp_after=$(run_orbit admin status --format json | jq -r .key_fingerprint)
assert_not_equals "${fp_before}" "${fp_after}" "Key fingerprint changed after secret rotation"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# ADM-07: Master Secret Vault Sealing & TOTP Reset
# ------------------------------------------------------------------------------
scenario_start "ADM-07" "Vault Sealing Integrity & TOTP Reset"
setup_test_sandbox "adm-07"

run_orbit admin init admin@manova.space --no-send --code 123456 >/dev/null

log_step "Issuing TOTP reset for user..."
totp_json=$(run_orbit admin totp reset user@manova.space --json)
assert_json_field "${totp_json}" ".status" "totp_reset" "TOTP reset status confirmed"
assert_json_field "${totp_json}" ".email" "user@manova.space" "TOTP reset email matches"

log_step "Testing permissions check on insecure vault (chmod 0777)..."
chmod 777 "${ORBIT_OWNER_STORE}"
insecure_status=$(run_orbit admin status)
assert_contains "${insecure_status}" "insecure" "Status warns on insecure vault permissions"

# Restore permissions
chmod 600 "${ORBIT_OWNER_STORE}"
secure_status=$(run_orbit admin status)
assert_contains "${secure_status}" "0600 (secure)" "Status restores 0600 secure display"

cleanup_test_sandbox
scenario_end

print_scenario_summary "01_admin.sh"
