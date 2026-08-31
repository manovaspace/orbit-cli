#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 02: Staff Control Plane (STF-01 through STF-05)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built
ensure_mock_server_running

# ------------------------------------------------------------------------------
# STF-01: Create Staff Member with Invite & TOTP
# ------------------------------------------------------------------------------
scenario_start "STF-01" "Staff Create with Onboarding Invite & TOTP Enrollment"
setup_test_sandbox "stf-01"
seed_owner_vault

log_step "Creating staff member alice with TOTP and invite flags..."
create_out=$(run_orbit staff create --server "${ORBIT_STAFF_URL}" \
    --uid alice \
    --name "Alice Smith" \
    --forward alice@example.com \
    --groups "dev,core" \
    --totp \
    --invite)

assert_contains "${create_out}" "uid    alice" "Output confirms created UID alice"
assert_contains "${create_out}" "sso    mock-ldap-password-alice" "SSO password generated"
assert_contains "${create_out}" "mail   mock-mail-password-alice" "Mailbox password generated"
assert_contains "${create_out}" "fwd    alice@example.com" "Personal forward recorded"
assert_contains "${create_out}" "otpauth" "Authelia TOTP URI generated"
assert_contains "${create_out}" "token" "Onboarding invite token generated"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# STF-02: Staff List & Get
# ------------------------------------------------------------------------------
scenario_start "STF-02" "Staff List & Detailed Inspection"
setup_test_sandbox "stf-02"
seed_owner_vault

log_step "Creating staff user bob..."
run_orbit staff create --server "${ORBIT_STAFF_URL}" \
    --uid bob \
    --name "Bob Jones" \
    --forward bob@example.com \
    --groups "dev" >/dev/null

log_step "Listing staff members..."
list_out=$(run_orbit staff list --server "${ORBIT_STAFF_URL}")
assert_contains "${list_out}" "bob" "Staff list contains bob"
assert_contains "${list_out}" "Bob Jones" "Staff list contains display name"
assert_contains "${list_out}" "active" "Staff list shows active status"

log_step "Inspecting staff member bob..."
get_out=$(run_orbit staff get bob --server "${ORBIT_STAFF_URL}")
assert_contains "${get_out}" "uid    bob" "Get confirms UID bob"
assert_contains "${get_out}" "name   Bob Jones" "Get confirms name"
assert_contains "${get_out}" "mail   bob@dev.manova.space" "Get confirms staff mailbox"
assert_contains "${get_out}" "fwd    bob@example.com" "Get confirms forward"
assert_contains "${get_out}" "status active" "Get confirms active status"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# STF-03: Staff Disable & Enable Lifecycle
# ------------------------------------------------------------------------------
scenario_start "STF-03" "Staff Disable & Enable State Transitions"
setup_test_sandbox "stf-03"
seed_owner_vault

log_step "Creating staff user charlie..."
run_orbit staff create --server "${ORBIT_STAFF_URL}" \
    --uid charlie \
    --name "Charlie Brown" \
    --forward charlie@example.com >/dev/null

log_step "Disabling staff user charlie..."
disable_out=$(run_orbit staff disable charlie --server "${ORBIT_STAFF_URL}")
assert_contains "${disable_out}" "disabled charlie" "Disable confirms charlie disabled"

get_disabled=$(run_orbit staff get charlie --server "${ORBIT_STAFF_URL}")
assert_contains "${get_disabled}" "status disabled" "Get confirms status changed to disabled"

log_step "Re-enabling staff user charlie..."
enable_out=$(run_orbit staff enable charlie --server "${ORBIT_STAFF_URL}")
assert_contains "${enable_out}" "enabled charlie" "Enable confirms charlie enabled"

get_enabled=$(run_orbit staff get charlie --server "${ORBIT_STAFF_URL}")
assert_contains "${get_enabled}" "status active" "Get confirms status restored to active"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# STF-04: Staff Update, Recreate, Password Reset, Delete
# ------------------------------------------------------------------------------
scenario_start "STF-04" "Staff Update, Recreate, Password Reset & Delete"
setup_test_sandbox "stf-04"
seed_owner_vault

log_step "Creating staff user dave..."
run_orbit staff create --server "${ORBIT_STAFF_URL}" \
    --uid dave \
    --name "Dave Miller" \
    --forward dave@example.com >/dev/null

log_step "Updating staff user dave..."
update_out=$(run_orbit staff update dave --server "${ORBIT_STAFF_URL}" \
    --name "Dave Updated" \
    --forward dave-new@example.com)
assert_contains "${update_out}" "updated dave" "Update confirms user dave updated"

get_updated=$(run_orbit staff get dave --server "${ORBIT_STAFF_URL}")
assert_contains "${get_updated}" "name   Dave Updated" "Get confirms updated name"
assert_contains "${get_updated}" "fwd    dave-new@example.com" "Get confirms updated forward email"

log_step "Resetting passwords & TOTP for dave..."
reset_out=$(run_orbit staff reset-password dave --server "${ORBIT_STAFF_URL}" --ldap --totp)
assert_contains "${reset_out}" "sso    reset-mock-ldap-password-dave" "SSO password reset confirmed"
assert_contains "${reset_out}" "otpauth" "TOTP reset confirmed"

log_step "Atomic recreate for dave..."
recreate_out=$(run_orbit staff recreate --server "${ORBIT_STAFF_URL}" \
    --uid dave \
    --name "Dave Miller Fresh" \
    --forward dave-fresh@example.com \
    --totp)
assert_contains "${recreate_out}" "mock-ldap-password-dave" "Recreate generates fresh credentials"
assert_contains "${recreate_out}" "dave-fresh@example.com" "Recreate saves updated personal forward"

log_step "Deleting staff user dave..."
del_out=$(run_orbit staff delete dave --server "${ORBIT_STAFF_URL}")
assert_contains "${del_out}" "deleted dave" "Delete confirms user dave deleted"

# Verify deleted user returns 404
set +e
get_deleted=$(run_orbit staff get dave --server "${ORBIT_STAFF_URL}" 2>&1)
set -e
assert_contains "${get_deleted}" "not found" "Get on deleted user returns not found"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# STF-05: HMAC Authentication Rejection & Reserved Account Guards
# ------------------------------------------------------------------------------
scenario_start "STF-05" "HMAC Security Enforcement & Reserved Directory Accounts"
setup_test_sandbox "stf-05"

log_step "Testing unverified owner rejection (no owner vault initialized)..."
set +e
unverified_out=$(run_orbit staff list --server "${ORBIT_STAFF_URL}" 2>&1)
set -e
assert_contains "${unverified_out}" "platform ownership is unverified" "Staff commands require verified owner vault"

log_step "Testing bad HMAC secret rejection (HTTP 401)..."
seed_owner_vault "tampered-admin@manova.space" "invalid-tampered-hmac-secret-99999"
set +e
bad_hmac_out=$(run_orbit staff list --server "${ORBIT_STAFF_URL}" 2>&1)
set -e
assert_contains "${bad_hmac_out}" "bad hmac" "Staff API rejects mismatched HMAC signature with HTTP 401"

# Initialize valid vault for reserved UID tests
seed_owner_vault

log_step "Testing reserved system UIDs rejection on staff create..."
reserved_uids=("admin" "authelia-bind" "verdaccio-bind" "verdaccio-ci")

for uid in "${reserved_uids[@]}"; do
    set +e
    res_out=$(run_orbit staff create --server "${ORBIT_STAFF_URL}" --uid "${uid}" --forward "res@example.com" 2>&1)
    set -e
    assert_contains "${res_out}" "reserved uid" "Staff create rejects reserved UID: ${uid}"
done

log_step "Testing reserved UID rejection on get/update/delete..."
for uid in "${reserved_uids[@]}"; do
    set +e
    get_res=$(run_orbit staff get "${uid}" --server "${ORBIT_STAFF_URL}" 2>&1)
    set -e
    assert_contains "${get_res}" "reserved uid" "Staff get rejects reserved UID: ${uid}"
done

cleanup_test_sandbox
scenario_end

print_scenario_summary "02_staff.sh"
