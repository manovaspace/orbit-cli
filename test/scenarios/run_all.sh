#!/usr/bin/env bash
# ==============================================================================
# Orbit CLI Comprehensive Test Scenarios — Master Driver Runner
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

START_TIME=$(date +%s)

echo -e "${COLOR_BOLD}╔══════════════════════════════════════════════════════════════════════════════╗${COLOR_RESET}"
echo -e "${COLOR_BOLD}║              ORBIT CLI COMPREHENSIVE SCENARIO TEST HARNESS                   ║${COLOR_RESET}"
echo -e "${COLOR_BOLD}║                   100% Branch & Matrix Coverage Suite                        ║${COLOR_RESET}"
echo -e "${COLOR_BOLD}╚══════════════════════════════════════════════════════════════════════════════╝${COLOR_RESET}"
echo ""

ensure_binaries_built
ensure_mock_server_running

SUITES=(
    "01_admin.sh"
    "02_staff.sh"
    "03_invite.sh"
    "04_onboard.sh"
    "05_doctor.sh"
    "06_assets.sh"
    "07_ports.sh"
    "08_env.sh"
    "09_config.sh"
    "10_workspace.sh"
)

TOTAL_SUITES=${#SUITES[@]}
PASSED_SUITES=0
FAILED_SUITES=0

declare -a SUITE_NAMES=()
declare -a SUITE_STATUS=()
declare -a SUITE_DURATIONS=()

for suite in "${SUITES[@]}"; do
    suite_path="${SCRIPT_DIR}/${suite}"
    if [ ! -f "${suite_path}" ]; then
        log_fail "Suite script not found: ${suite}"
        FAILED_SUITES=$((FAILED_SUITES + 1))
        SUITE_NAMES+=("${suite}")
        SUITE_STATUS+=("MISSING")
        SUITE_DURATIONS+=("0s")
        continue
    fi
    
    suite_start=$(date +%s)
    echo ""
    log_step "Executing test suite: ${suite}..."
    
    set +e
    bash "${suite_path}"
    suite_ec=$?
    set -e
    
    suite_end=$(date +%s)
    duration=$((suite_end - suite_start))
    
    SUITE_NAMES+=("${suite}")
    SUITE_DURATIONS+=("${duration}s")
    
    if [ ${suite_ec} -eq 0 ]; then
        PASSED_SUITES=$((PASSED_SUITES + 1))
        SUITE_STATUS+=("PASSED")
        log_pass "Suite ${suite} completed successfully (${duration}s)"
    else
        FAILED_SUITES=$((FAILED_SUITES + 1))
        SUITE_STATUS+=("FAILED")
        log_fail "Suite ${suite} failed with exit code ${suite_ec} (${duration}s)"
    fi
done

END_TIME=$(date +%s)
TOTAL_DURATION=$((END_TIME - START_TIME))

echo ""
echo -e "${COLOR_BOLD}==============================================================================${COLOR_RESET}"
echo -e "${COLOR_BOLD}                      FINAL SCENARIO EXECUTION MATRIX                         ${COLOR_RESET}"
echo -e "${COLOR_BOLD}==============================================================================${COLOR_RESET}"
printf "  ${COLOR_BOLD}%-24s %-16s %-12s${COLOR_RESET}\n" "SUITE" "STATUS" "DURATION"
echo -e "  --------------------------------------------------"

for i in "${!SUITE_NAMES[@]}"; do
    s_name="${SUITE_NAMES[$i]}"
    s_stat="${SUITE_STATUS[$i]}"
    s_dur="${SUITE_DURATIONS[$i]}"
    
    if [ "${s_stat}" = "PASSED" ]; then
        printf "  %-24s ${COLOR_GREEN}%-16s${COLOR_RESET} %-12s\n" "${s_name}" "✔ PASSED" "${s_dur}"
    else
        printf "  %-24s ${COLOR_RED}%-16s${COLOR_RESET} %-12s\n" "${s_name}" "✖ FAILED" "${s_dur}"
    fi
done

echo -e "  --------------------------------------------------"
echo -e "  Total Suites:    ${TOTAL_SUITES}"
echo -e "  ${COLOR_GREEN}✔ Passed Suites:   ${PASSED_SUITES}${COLOR_RESET}"
if [ "${FAILED_SUITES}" -gt 0 ]; then
    echo -e "  ${COLOR_RED}✖ Failed Suites:   ${FAILED_SUITES}${COLOR_RESET}"
else
    echo -e "  ${COLOR_DIM}✖ Failed Suites:   0${COLOR_RESET}"
fi
echo -e "  Total Duration:  ${TOTAL_DURATION}s"
echo -e "${COLOR_BOLD}==============================================================================${COLOR_RESET}"

if [ "${FAILED_SUITES}" -gt 0 ]; then
    exit 1
fi

exit 0
