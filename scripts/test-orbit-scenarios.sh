#!/usr/bin/env bash
# ==============================================================================
# Orbit CLI: Host-Side One-Click Testbed Runner & Scenario Orchestrator
# ==============================================================================

set -euo pipefail

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
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/test/testbed/docker-compose.test.yml"
SCENARIOS_DIR="${REPO_ROOT}/test/scenarios"

# Defaults
MODE="container"
SUITE_ARG=""
REBUILD=false
KEEP_CONTAINERS=false
CLEANUP_DONE=0

# Logging helpers
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

# Print usage help
show_help() {
    echo -e "${COLOR_BOLD}Orbit CLI Scenario Testbed Runner${COLOR_RESET}"
    echo ""
    echo -e "${COLOR_BOLD}Usage:${COLOR_RESET}"
    echo "  ./scripts/test-orbit-scenarios.sh [OPTIONS]"
    echo ""
    echo -e "${COLOR_BOLD}Options:${COLOR_RESET}"
    echo "  -l, --local                 Run scenarios directly on the host (with local mock server)"
    echo "  -s, --suite <suite-name>    Run a specific scenario suite only (e.g. 01_admin.sh or 04_onboard.sh)"
    echo "  -r, --rebuild               Force rebuild Docker container images before running"
    echo "  -k, --keep                  Keep containers running after test execution (for debugging)"
    echo "  -h, --help                  Show this help message and exit"
    echo ""
    echo -e "${COLOR_BOLD}Available Suites in ${SCENARIOS_DIR}:${COLOR_RESET}"
    echo "  01_admin.sh      Platform Ownership & Admin (ADM-01..ADM-07)"
    echo "  02_staff.sh      Staff IAM & Access Lifecycle (STF-01..STF-05)"
    echo "  03_invite.sh     Staff Invitation System (INV-01..INV-03)"
    echo "  04_onboard.sh    Staff Onboarding Workflow (ONB-01..ONB-03)"
    echo "  05_doctor.sh     Doctor Diagnostics & Repair (DOC-01..DOC-02)"
    echo "  06_assets.sh     Asset Management & Sync (AST-01..AST-02)"
    echo "  07_ports.sh      Port Block Allocation (PRT-01)"
    echo "  08_env.sh        Environment Schema & Sync (ENV-01)"
    echo "  09_config.sh     Configuration Storage (CFG-01)"
    echo "  10_workspace.sh  Multi-Repo Workspace Orchestration (WKS-01)"
    echo ""
    echo -e "${COLOR_BOLD}Examples:${COLOR_RESET}"
    echo "  ./scripts/test-orbit-scenarios.sh                      # Full matrix in isolated Docker testbed"
    echo "  ./scripts/test-orbit-scenarios.sh --local              # Full matrix directly on host"
    echo "  ./scripts/test-orbit-scenarios.sh --suite 01_admin.sh  # Run Suite 01 in Docker testbed"
    echo "  ./scripts/test-orbit-scenarios.sh -l -s 04_onboard.sh  # Run Suite 04 locally on host"
    echo "  ./scripts/test-orbit-scenarios.sh --rebuild            # Rebuild images without cache and run"
    echo "  ./scripts/test-orbit-scenarios.sh --keep               # Retain containers for inspection"
}

# Parse command line options
while [[ $# -gt 0 ]]; do
    case "$1" in
        -h|--help)
            show_help
            exit 0
            ;;
        -l|--local)
            MODE="local"
            shift
            ;;
        -r|--rebuild)
            REBUILD=true
            shift
            ;;
        -k|--keep)
            KEEP_CONTAINERS=true
            shift
            ;;
        -s|--suite)
            if [[ $# -lt 2 || -z "$2" ]]; then
                log_fail "Option '$1' requires a suite name argument."
                exit 1
            fi
            SUITE_ARG="$2"
            shift 2
            ;;
        --suite=*)
            SUITE_ARG="${1#*=}"
            shift
            ;;
        *)
            log_fail "Unknown argument: $1"
            echo ""
            show_help
            exit 1
            ;;
    esac
done

# Validate suite argument if provided
TARGET_SUITE=""
if [[ -n "${SUITE_ARG}" ]]; then
    SUITE_BASENAME="$(basename "${SUITE_ARG}")"
    if [[ "${SUITE_BASENAME}" != *.sh ]]; then
        SUITE_BASENAME="${SUITE_BASENAME}.sh"
    fi
    if [[ ! -f "${SCENARIOS_DIR}/${SUITE_BASENAME}" ]]; then
        log_fail "Scenario suite '${SUITE_ARG}' not found in ${SCENARIOS_DIR}"
        echo ""
        echo "Available suites:"
        for s in "${SCENARIOS_DIR}"/[0-9]*.sh; do
            [ -f "$s" ] && echo "  - $(basename "$s")"
        done
        exit 1
    fi
    TARGET_SUITE="${SUITE_BASENAME}"
fi

# Check Docker prerequisites for container mode
check_docker_prerequisites() {
    if ! command -v docker >/dev/null 2>&1; then
        log_fail "Docker executable not found in PATH."
        log_info "Please install Docker or use --local to run tests directly on the host."
        exit 1
    fi

    if ! docker compose version >/dev/null 2>&1; then
        log_fail "Docker Compose (v2) not available."
        log_info "Please install the Docker Compose plugin or use --local."
        exit 1
    fi

    if ! docker info >/dev/null 2>&1; then
        log_fail "Docker daemon is unreachable or current user lacks docker group permissions."
        log_info "Ensure Docker daemon is active or run with --local."
        exit 1
    fi

    if [[ ! -f "${COMPOSE_FILE}" ]]; then
        log_fail "Docker compose testbed definition not found at: ${COMPOSE_FILE}"
        exit 1
    fi
}

# Cleanup trap handler
cleanup_containers() {
    local ec=$?
    if [[ "${CLEANUP_DONE}" -eq 1 ]]; then
        return
    fi
    CLEANUP_DONE=1

    if [[ "${KEEP_CONTAINERS}" = "true" ]]; then
        echo ""
        log_warn "Containers kept running as requested by --keep. To shut down manually, run:"
        log_warn "  docker compose -f \"${COMPOSE_FILE}\" down -v"
    else
        echo ""
        log_step "Tearing down isolated testbed containers and networks..."
        docker compose -f "${COMPOSE_FILE}" down -v >/dev/null 2>&1 || true
        log_pass "Testbed teardown complete."
    fi
    exit "${ec}"
}

echo -e "${COLOR_BOLD}╔══════════════════════════════════════════════════════════════════════════════╗${COLOR_RESET}"
echo -e "${COLOR_BOLD}║                ORBIT CLI SCENARIOS TESTBED RUNNER                            ║${COLOR_RESET}"
echo -e "${COLOR_BOLD}╚══════════════════════════════════════════════════════════════════════════════╝${COLOR_RESET}"
log_info "Execution Mode: ${MODE}"
if [[ -n "${TARGET_SUITE}" ]]; then
    log_info "Target Suite:   ${TARGET_SUITE}"
else
    log_info "Target Suite:   ALL SUITES (run_all.sh)"
fi
echo ""

# Execution Dispatch
if [[ "${MODE}" = "local" ]]; then
    log_step "Starting local scenario execution on host..."
    if [[ -n "${TARGET_SUITE}" ]]; then
        bash "${SCENARIOS_DIR}/${TARGET_SUITE}"
    else
        bash "${SCENARIOS_DIR}/run_all.sh"
    fi
    exit_code=$?
    exit "${exit_code}"
else
    check_docker_prerequisites

    trap cleanup_containers EXIT INT TERM

    if [[ "${REBUILD}" = "true" ]]; then
        log_step "Building Docker testbed images (--no-cache)..."
        docker compose -f "${COMPOSE_FILE}" build --no-cache
    else
        log_step "Ensuring Docker testbed images are built..."
        docker compose -f "${COMPOSE_FILE}" build
    fi

    log_step "Starting isolated testbed services (orbit-test-edge & orbit-test-client)..."
    docker compose -f "${COMPOSE_FILE}" up -d --wait

    echo ""
    if [[ -n "${TARGET_SUITE}" ]]; then
        log_step "Executing scenario suite '${TARGET_SUITE}' inside orbit-test-client..."
        set +e
        docker compose -f "${COMPOSE_FILE}" exec -T orbit-test-client bash "test/scenarios/${TARGET_SUITE}"
        exit_code=$?
        set -e
    else
        log_step "Executing full scenario matrix inside orbit-test-client..."
        set +e
        docker compose -f "${COMPOSE_FILE}" exec -T orbit-test-client bash test/scenarios/run_all.sh
        exit_code=$?
        set -e
    fi

    if [[ "${exit_code}" -eq 0 ]]; then
        echo ""
        log_pass "Scenario execution in testbed completed successfully!"
    else
        echo ""
        log_fail "Scenario execution in testbed failed with exit code ${exit_code}."
    fi

    exit "${exit_code}"
fi
