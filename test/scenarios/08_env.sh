#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 08: Environment Contracts & Secrets Management (ENV-01)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built

# ------------------------------------------------------------------------------
# ENV-01: Environment Contract Validation & Schema Check
# ------------------------------------------------------------------------------
scenario_start "ENV-01" "Environment Contracts & Schema Validation"
setup_test_sandbox "env-01"

WORKSPACE_DIR="${SANDBOX_DIR}/workspace"
export ORBIT_WORKSPACE="${WORKSPACE_DIR}"
mkdir -p "${WORKSPACE_DIR}/services/backend"

log_step "Creating .env.schema.yaml contract..."
cat > "${WORKSPACE_DIR}/services/backend/.env.schema.yaml" <<EOF
version: "1"
variables:
  - name: DATABASE_URL
    required: true
    description: "Database connection URI"
    default: "postgres://orbit:secret@localhost:5432/orbit_db"
  - name: API_KEY
    required: true
    description: "External API access token"
    default: "mock-key-12345"
  - name: DEBUG_MODE
    required: false
    default: "true"
EOF

log_step "Running orbit env setup to generate .env from schema..."
setup_out=$(run_orbit env setup "${WORKSPACE_DIR}")
assert_contains "${setup_out}" "Orbit Environment Setup" "Env setup header displayed"
assert_contains "${setup_out}" "generated .env" "Generated .env confirmation displayed"

ENV_FILE="${WORKSPACE_DIR}/services/backend/.env"
assert_file_exists "${ENV_FILE}" ".env file generated on disk"
assert_file_mode "${ENV_FILE}" "600" ".env has secure 0600 permissions"

env_content=$(cat "${ENV_FILE}")
assert_contains "${env_content}" "DATABASE_URL=postgres://orbit:secret@localhost:5432/orbit_db" ".env populated with defaults"

log_step "Checking environment contract compliance with orbit env check..."
check_out=$(run_orbit env check "${WORKSPACE_DIR}")
assert_contains "${check_out}" ".env valid" "Validation confirms .env is compliant"

log_step "Testing validation failure when required variable is missing..."
cat > "${ENV_FILE}" <<EOF
DEBUG_MODE=true
EOF

set +e
invalid_check=$(run_orbit env check "${WORKSPACE_DIR}" 2>&1)
set -e
assert_contains "${invalid_check}" "validation issue" "Validation correctly flags missing required variables"
assert_contains "${invalid_check}" "DATABASE_URL" "Error output specifies missing DATABASE_URL"

cleanup_test_sandbox
scenario_end

print_scenario_summary "08_env.sh"
