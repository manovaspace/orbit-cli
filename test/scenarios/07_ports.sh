#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 07: Port Block Allocation & Inspection (PRT-01)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built

# ------------------------------------------------------------------------------
# PRT-01: Port Block Allocation, Conflict Detection & Inspection
# ------------------------------------------------------------------------------
scenario_start "PRT-01" "Port Manager 50-Port Block Allocation (ADR-006)"
setup_test_sandbox "prt-01"

log_step "Listing registered project 50-port blocks..."
list_out=$(run_orbit port list)
assert_contains "${list_out}" "Orbit Port Manager" "Port list header displayed"
assert_contains "${list_out}" "Deterministic Slots (0-9)" "Deterministic slots 0-9 displayed"
assert_contains "${list_out}" "Dynamic Slots (10-49)" "Dynamic slots 10-49 displayed"

log_step "Dynamically allocating port for service in orbit-platform project..."
alloc_out=$(run_orbit port allocate orbit-platform worker-service)
assert_contains "${alloc_out}" "Port Allocation Successful" "Allocation confirms success"
assert_contains "${alloc_out}" "worker-service" "Service name displayed in allocation"
assert_contains "${alloc_out}" "export WORKER_SERVICE_PORT=" "Environment variable export command provided"

log_step "Testing error handling on unknown project allocation..."
set +e
err_out=$(run_orbit port allocate non-existent-project-xyz my-svc 2>&1)
set -e
assert_contains "${err_out}" "unknown project" "Helpful error message on invalid project"

cleanup_test_sandbox
scenario_end

print_scenario_summary "07_ports.sh"
