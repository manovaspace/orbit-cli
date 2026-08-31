#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 05: System Diagnostics & Auto-Healing (DOC-01, DOC-02)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built

# ------------------------------------------------------------------------------
# DOC-01: System Diagnostic Check
# ------------------------------------------------------------------------------
scenario_start "DOC-01" "System Diagnostics & Pre-Flight Health Inspection"
setup_test_sandbox "doc-01"

log_step "Running system diagnostics in standard table format..."
doc_table=$(run_orbit doctor)
assert_contains "${doc_table}" "Orbit System Doctor" "Doctor title displayed"
assert_contains "${doc_table}" "Host" "Host check present in results"
assert_contains "${doc_table}" "Git" "Git check present in results"

log_step "Running system diagnostics in JSON format..."
doc_json=$(run_orbit doctor --json 2>&1 || true)
assert_contains "${doc_json}" "results" "Doctor JSON has results key"
assert_contains "${doc_json}" "status" "Doctor JSON contains status key"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# DOC-02: Auto-Healing Mode (--fix)
# ------------------------------------------------------------------------------
scenario_start "DOC-02" "Auto-Healing Diagnostics & Remediation (--fix)"
setup_test_sandbox "doc-02"

log_step "Running doctor with --fix in non-interactive mode..."
fix_out=$(run_orbit doctor --fix --non-interactive || true)
assert_contains "${fix_out}" "Orbit System Doctor" "Doctor executes auto-fix mode"

log_step "Running doctor with --fix and --json..."
fix_json=$(run_orbit doctor --fix --json || true)
assert_contains "${fix_json}" "results" "Auto-heal JSON has diagnostic results"

cleanup_test_sandbox
scenario_end

print_scenario_summary "05_doctor.sh"
