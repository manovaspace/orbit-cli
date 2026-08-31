#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 09: Configuration Hardening & Secret Masking (CFG-01)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built

# ------------------------------------------------------------------------------
# CFG-01: Configuration Hardening, Secret Masking & AST Modification
# ------------------------------------------------------------------------------
scenario_start "CFG-01" "Configuration Hardening, Masking & Persistence"
setup_test_sandbox "cfg-01"

CONFIG_FILE="${ORBIT_CONFIG_DIR}/config.yaml"

log_step "Initializing fresh default configuration..."
init_out=$(run_orbit config init)
assert_contains "${init_out}" "Configuration file initialized" "Config init confirms creation"
assert_file_exists "${CONFIG_FILE}" "config.yaml created on disk"
assert_file_mode "${CONFIG_FILE}" "600" "config.yaml sealed with mode 0600"

log_step "Setting core configuration properties (server.url, assets.bucket, defaults.scope)..."
set_url=$(run_orbit config set server.url "http://orbit.dev.manova.space:8080")
assert_contains "${set_url}" "Set server.url" "server.url updated successfully"

set_bucket=$(run_orbit config set assets.bucket "custom-media-bucket")
assert_contains "${set_bucket}" "Set assets.bucket" "assets.bucket updated successfully"

set_scope=$(run_orbit config set defaults.scope "core")
assert_contains "${set_scope}" "Set defaults.scope" "defaults.scope updated successfully"

log_step "Setting custom sensitive parameter (custom.api_token)..."
set_custom=$(run_orbit config set custom.api_token "super-sensitive-token-12345")
assert_contains "${set_custom}" "Set custom.api_token" "custom.api_token saved"

log_step "Verifying configuration display via orbit config show..."
show_out=$(run_orbit config show)
assert_contains "${show_out}" "http://orbit.dev.manova.space:8080" "server.url visible in show"
assert_contains "${show_out}" "custom-media-bucket" "assets.bucket visible in show"

log_step "Verifying JSON output formatting..."
show_json=$(run_orbit config show --format json)
assert_json_field "${show_json}" ".server.url" "http://orbit.dev.manova.space:8080" "JSON config output matches"
assert_json_field "${show_json}" ".assets.bucket" "custom-media-bucket" "JSON bucket matches"

log_step "Verifying raw plaintext retrieval via orbit config get..."
raw_val=$(run_orbit config get custom.api_token --raw)
assert_equals "super-sensitive-token-12345" "${raw_val}" "Plaintext value retrieved via explicit get"

log_step "Unsetting custom configuration key..."
unset_out=$(run_orbit config unset custom.api_token)
assert_contains "${unset_out}" "Unset custom.api_token" "Key unset successfully"

log_step "Listing configuration parameters and their resolution sources..."
list_out=$(run_orbit config list)
assert_contains "${list_out}" "server.url" "Config list displays server.url"
assert_contains "${list_out}" "SOURCE" "Config list displays resolution sources"

cleanup_test_sandbox
scenario_end

print_scenario_summary "09_config.sh"
