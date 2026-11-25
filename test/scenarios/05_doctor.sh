#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 05: System Diagnostics & Auto-Healing (DOC-01, DOC-02)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built

# ------------------------------------------------------------------------------
# DOC-01: Setup Check (check only, never changes the machine)
# ------------------------------------------------------------------------------
scenario_start "DOC-01" "Setup Check Without Changes"
setup_test_sandbox "doc-01"

log_step "Running o doctor in JSON format..."
doc_json=$(run_orbit doctor --json 2>&1 || true)
assert_contains "${doc_json}" "parts" "Doctor JSON has parts key"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# DOC-02: --fix is refused (o doctor never changes the machine)
# ------------------------------------------------------------------------------
scenario_start "DOC-02" "Doctor Refuses --fix"
setup_test_sandbox "doc-02"

log_step "Running doctor with --fix..."
fix_out=$(run_orbit doctor --fix 2>&1 || true)
assert_contains "${fix_out}" "o doctor never changes the machine; run o setup" "Doctor --fix is refused with a usage error"

cleanup_test_sandbox
scenario_end

print_scenario_summary "05_doctor.sh"
