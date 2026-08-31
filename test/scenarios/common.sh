#!/usr/bin/env bash
# ==============================================================================
# Orbit CLI Scenario Test Harness — Common Utilities & Assertions
# ==============================================================================

set -uo pipefail

# ANSI color codes
COLOR_RESET="\033[0m"
COLOR_BOLD="\033[1m"
COLOR_RED="\033[31m"
COLOR_GREEN="\033[32m"
COLOR_YELLOW="\033[33m"
COLOR_BLUE="\033[34m"
COLOR_CYAN="\033[36m"
COLOR_DIM="\033[2m"

# Resolve absolute paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
BIN_DIR="${REPO_ROOT}/bin"
ORBIT_BIN="${BIN_DIR}/orbit"
MOCK_BIN="${BIN_DIR}/mockserver"

# Service endpoints (defaults for local or container testbed)
ORBIT_API_URL="${ORBIT_API_URL:-http://127.0.0.1:8080}"
ORBIT_STAFF_URL="${ORBIT_STAFF_URL:-http://127.0.0.1:10800}"
ORBIT_S3_ENDPOINT="${ORBIT_S3_ENDPOINT:-http://127.0.0.1:9000}"
ORBIT_FORGEJO_URL="${ORBIT_FORGEJO_URL:-http://127.0.0.1:3000}"
ORBIT_STAFF_HMAC_SECRET="${ORBIT_STAFF_HMAC_SECRET:-orbit-dev-insecure-staff-hmac-secret-32bytes}"
ORBIT_SIGNING_SECRET="${ORBIT_SIGNING_SECRET:-orbit-dev-insecure-invitation-signing-secret-key-32bytes}"

# Global Counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
CURRENT_SCENARIO=""
MOCK_SERVER_PID=""

# Logging functions
log_info() {
    echo -e "${COLOR_BLUE}ℹ [INFO]${COLOR_RESET} $*"
}

log_pass() {
    echo -e "${COLOR_GREEN}✔ [PASS]${COLOR_RESET} $*"
}

log_fail() {
    echo -e "${COLOR_RED}✖ [FAIL]${COLOR_RESET} $*"
}

log_warn() {
    echo -e "${COLOR_YELLOW}⚠ [WARN]${COLOR_RESET} $*"
}

log_step() {
    echo -e "${COLOR_CYAN}▶ [STEP]${COLOR_RESET} ${COLOR_BOLD}$*${COLOR_RESET}"
}

# Scenario lifecycle markers
scenario_start() {
    local id="$1"
    local desc="$2"
    CURRENT_SCENARIO="${id}"
    echo ""
    echo -e "${COLOR_BOLD}==============================================================================${COLOR_RESET}"
    echo -e "${COLOR_CYAN}${COLOR_BOLD}SCENARIO ${id}:${COLOR_RESET} ${COLOR_BOLD}${desc}${COLOR_RESET}"
    echo -e "${COLOR_BOLD}==============================================================================${COLOR_RESET}"
}

scenario_end() {
    echo ""
}

# ------------------------------------------------------------------------------
# Test Assertions
# ------------------------------------------------------------------------------

assert_equals() {
    local expected="$1"
    local actual="$2"
    local desc="${3:-Values should be equal}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "${expected}" = "${actual}" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc}"
        echo -e "   ${COLOR_DIM}Expected:${COLOR_RESET} ${COLOR_GREEN}${expected}${COLOR_RESET}"
        echo -e "   ${COLOR_DIM}Actual:  ${COLOR_RESET} ${COLOR_RED}${actual}${COLOR_RESET}"
        return 1
    fi
}

assert_not_equals() {
    local unexpected="$1"
    local actual="$2"
    local desc="${3:-Values should not be equal}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "${unexpected}" != "${actual}" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc}"
        echo -e "   ${COLOR_DIM}Expected not:${COLOR_RESET} ${COLOR_RED}${unexpected}${COLOR_RESET}"
        echo -e "   ${COLOR_DIM}Actual:      ${COLOR_RESET} ${COLOR_RED}${actual}${COLOR_RESET}"
        return 1
    fi
}

assert_contains() {
    local haystack="$1"
    local needle="$2"
    local desc="${3:-String should contain substring}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ "${haystack}" == *"${needle}"* ]]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc}"
        echo -e "   ${COLOR_DIM}Needle not found:${COLOR_RESET} ${COLOR_RED}${needle}${COLOR_RESET}"
        echo -e "   ${COLOR_DIM}Haystack:${COLOR_RESET}\n${haystack}"
        return 1
    fi
}

assert_not_contains() {
    local haystack="$1"
    local needle="$2"
    local desc="${3:-String should not contain substring}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ "${haystack}" != *"${needle}"* ]]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc}"
        echo -e "   ${COLOR_DIM}Forbidden needle found:${COLOR_RESET} ${COLOR_RED}${needle}${COLOR_RESET}"
        return 1
    fi
}

assert_file_exists() {
    local path="$1"
    local desc="${2:-File should exist: ${path}}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ -f "${path}" ] || [ -d "${path}" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc} (File not found: ${path})"
        return 1
    fi
}

assert_file_not_exists() {
    local path="$1"
    local desc="${2:-File should not exist: ${path}}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ ! -e "${path}" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc} (File unexpectedly exists: ${path})"
        return 1
    fi
}

assert_file_mode() {
    local path="$1"
    local expected_mode="$2"
    local desc="${3:-File permissions should be ${expected_mode}}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ ! -e "${path}" ]; then
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc} (File does not exist: ${path})"
        return 1
    fi
    
    local actual_mode
    if stat -c "%a" "${path}" >/dev/null 2>&1; then
        actual_mode=$(stat -c "%a" "${path}")
    else
        actual_mode=$(stat -f "%Lp" "${path}" 2>/dev/null || echo "unknown")
    fi
    
    if [ "${actual_mode}" = "${expected_mode}" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc}"
        echo -e "   ${COLOR_DIM}Expected mode:${COLOR_RESET} ${COLOR_GREEN}${expected_mode}${COLOR_RESET}"
        echo -e "   ${COLOR_DIM}Actual mode:  ${COLOR_RESET} ${COLOR_RED}${actual_mode}${COLOR_RESET}"
        return 1
    fi
}

assert_json_field() {
    local json_str="$1"
    local field="$2"
    local expected="$3"
    local desc="${4:-JSON field ${field} should equal ${expected}}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    local actual
    if command -v jq >/dev/null 2>&1; then
        actual=$(echo "${json_str}" | jq -r "${field}" 2>/dev/null || echo "null")
    else
        actual=$(python3 -c "import sys, json; data=json.loads(sys.stdin.read()); print(data${field})" <<< "${json_str}" 2>/dev/null || echo "null")
    fi
    
    if [ "${actual}" = "${expected}" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc}"
        echo -e "   ${COLOR_DIM}Expected JSON field ${field}:${COLOR_RESET} ${COLOR_GREEN}${expected}${COLOR_RESET}"
        echo -e "   ${COLOR_DIM}Actual:                       ${COLOR_RESET} ${COLOR_RED}${actual}${COLOR_RESET}"
        return 1
    fi
}

assert_exit_code() {
    local expected_code="$1"
    local cmd="$2"
    local desc="${3:-Command exit code should be ${expected_code}}"
    
    TESTS_RUN=$((TESTS_RUN + 1))
    set +e
    eval "${cmd}" >/dev/null 2>&1
    local actual_code=$?
    set -e
    
    if [ "${actual_code}" -eq "${expected_code}" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_pass "${desc}"
        return 0
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        log_fail "${desc}"
        echo -e "   ${COLOR_DIM}Command:      ${COLOR_RESET} ${cmd}"
        echo -e "   ${COLOR_DIM}Expected code:${COLOR_RESET} ${COLOR_GREEN}${expected_code}${COLOR_RESET}"
        echo -e "   ${COLOR_DIM}Actual code:  ${COLOR_RESET} ${COLOR_RED}${actual_code}${COLOR_RESET}"
        return 1
    fi
}

# ------------------------------------------------------------------------------
# Sandbox & Isolation Management
# ------------------------------------------------------------------------------

setup_test_sandbox() {
    local name="${1:-sandbox}"
    SANDBOX_DIR=$(mktemp -d -t "orbit-test-${name}-XXXXXX")
    
    export HOME="${SANDBOX_DIR}/home"
    export XDG_CONFIG_HOME="${HOME}/.config"
    export ORBIT_CONFIG_DIR="${XDG_CONFIG_HOME}/orbit"
    export ORBIT_OWNER_STORE="${ORBIT_CONFIG_DIR}/owner.json"
    export ORBIT_SESSION_FILE="${ORBIT_CONFIG_DIR}/onboard-session.json"
    export ORBIT_INVITE_STORE="${ORBIT_CONFIG_DIR}/invites.json"
    export ORBIT_R2_ENV="${ORBIT_CONFIG_DIR}/r2.env"
    export ORBIT_NON_INTERACTIVE="1"
    export ORBIT_API_URL="${ORBIT_API_URL}"
    export ORBIT_STAFF_URL="${ORBIT_STAFF_URL}"
    export ORBIT_STAFF_HMAC_SECRET="${ORBIT_STAFF_HMAC_SECRET}"
    export ORBIT_SIGNING_SECRET="${ORBIT_SIGNING_SECRET}"
    export MANOVA_INVITE_SECRET="${ORBIT_SIGNING_SECRET}"
    export ORBIT_TESTBED="1"
    export ORBIT_SKIP_HOSTGATE="1"
    export PATH="${HOME}/.local/bin:${BIN_DIR}:${PATH}"
    
    mkdir -p "${HOME}/.ssh" "${HOME}/.local/bin" "${ORBIT_CONFIG_DIR}" "${SANDBOX_DIR}/workspace"
    chmod 700 "${HOME}" "${HOME}/.ssh"
    
    log_info "Initialized test sandbox at ${SANDBOX_DIR}"
}

seed_owner_vault() {
    local email="${1:-admin@manova.space}"
    local secret="${2:-${ORBIT_STAFF_HMAC_SECRET}}"
    mkdir -p "${ORBIT_CONFIG_DIR}"
    cat > "${ORBIT_OWNER_STORE}" <<EOF
{
  "email": "${email}",
  "display_name": "Platform Admin",
  "verified_at": "2026-08-31T00:00:00Z",
  "root_signing_secret": "${secret}",
  "key_fingerprint": "SHA256:mock-verified-fingerprint"
}
EOF
    chmod 600 "${ORBIT_OWNER_STORE}"
    log_info "Seeded owner vault for ${email}"
}

cleanup_test_sandbox() {
    if [ -n "${SANDBOX_DIR:-}" ] && [ -d "${SANDBOX_DIR}" ]; then
        rm -rf "${SANDBOX_DIR}"
        log_info "Cleaned up test sandbox ${SANDBOX_DIR}"
    fi
}

# ------------------------------------------------------------------------------
# Orbit Binary & Mock Server Helpers
# ------------------------------------------------------------------------------

ensure_binaries_built() {
    if [ ! -f "${ORBIT_BIN}" ]; then
        log_info "Compiling orbit binary..."
        mkdir -p "${BIN_DIR}"
        (cd "${REPO_ROOT}" && go build -o "${ORBIT_BIN}" ./cmd/orbit)
    fi
    if [ ! -f "${MOCK_BIN}" ]; then
        log_info "Compiling mockserver daemon..."
        mkdir -p "${BIN_DIR}"
        (cd "${REPO_ROOT}" && go build -o "${MOCK_BIN}" ./test/testbed/mockserver)
    fi
}

ensure_mock_server_running() {
    # Check if edge server is already answering
    if curl -s -f -m 1 "${ORBIT_API_URL}/healthz" >/dev/null 2>&1; then
        log_info "Edge mock service already running at ${ORBIT_API_URL}"
        return 0
    fi

    ensure_binaries_built
    
    local edge_p="${ORBIT_MOCK_EDGE_PORT:-18080}"
    local staff_p="${ORBIT_MOCK_STAFF_PORT:-18800}"
    local s3_p="${ORBIT_MOCK_S3_PORT:-19000}"
    local forgejo_p="${ORBIT_MOCK_FORGEJO_PORT:-13000}"

    export ORBIT_API_URL="http://127.0.0.1:${edge_p}"
    export ORBIT_STAFF_URL="http://127.0.0.1:${staff_p}"
    export ORBIT_S3_ENDPOINT="http://127.0.0.1:${s3_p}"
    export ORBIT_FORGEJO_URL="http://127.0.0.1:${forgejo_p}"

    log_info "Starting mock server daemon in background on ports (${edge_p}, ${staff_p}, ${s3_p}, ${forgejo_p})..."
    "${MOCK_BIN}" \
        -edge-addr=":${edge_p}" \
        -staff-addr=":${staff_p}" \
        -s3-addr=":${s3_p}" \
        -forgejo-addr=":${forgejo_p}" \
        -hmac-secret="${ORBIT_STAFF_HMAC_SECRET}" \
        -invite-secret="${ORBIT_SIGNING_SECRET}" >/dev/null 2>&1 &
    
    MOCK_SERVER_PID=$!
    
    # Wait for mock server healthz
    local attempts=0
    while ! curl -s -f -m 1 "${ORBIT_API_URL}/healthz" >/dev/null 2>&1; do
        sleep 0.1
        attempts=$((attempts + 1))
        if [ "${attempts}" -ge 50 ]; then
            log_fail "Failed to start mock server daemon on ${ORBIT_API_URL} within 5 seconds"
            return 1
        fi
    done
    
    log_pass "Mock server active at ${ORBIT_API_URL} (PID: ${MOCK_SERVER_PID})"
    
    # Register exit trap to kill background mock server
    trap 'stop_local_mock_server' EXIT INT TERM
}

stop_local_mock_server() {
    if [ -n "${MOCK_SERVER_PID}" ]; then
        kill "${MOCK_SERVER_PID}" 2>/dev/null || true
        wait "${MOCK_SERVER_PID}" 2>/dev/null || true
        MOCK_SERVER_PID=""
    fi
}

run_orbit() {
    "${ORBIT_BIN}" "$@"
}

# Print Scenario Summary
print_scenario_summary() {
    local suite_name="${1:-Scenario Suite}"
    echo ""
    echo -e "${COLOR_BOLD}------------------------------------------------------------------------------${COLOR_RESET}"
    echo -e "${COLOR_BOLD}SUMMARY: ${suite_name}${COLOR_RESET}"
    echo -e "  Total Assertions: ${TESTS_RUN}"
    echo -e "  ${COLOR_GREEN}✔ Passed:           ${TESTS_PASSED}${COLOR_RESET}"
    if [ "${TESTS_FAILED}" -gt 0 ]; then
        echo -e "  ${COLOR_RED}✖ Failed:           ${TESTS_FAILED}${COLOR_RESET}"
    else
        echo -e "  ${COLOR_DIM}✖ Failed:           0${COLOR_RESET}"
    fi
    echo -e "${COLOR_BOLD}------------------------------------------------------------------------------${COLOR_RESET}"
    
    if [ "${TESTS_FAILED}" -gt 0 ]; then
        return 1
    fi
    return 0
}
