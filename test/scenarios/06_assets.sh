#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 06: Media Assets Synchronization (AST-01, AST-02)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built
ensure_mock_server_running

# ------------------------------------------------------------------------------
# AST-01: Asset Add, Status, Push & Pull with S3 Mock
# ------------------------------------------------------------------------------
scenario_start "AST-01" "Media Assets Add, Push, Status & Pull Lifecycle"
setup_test_sandbox "ast-01"

log_step "Configuring mock R2 environment..."
cat > "${ORBIT_R2_ENV}" <<EOF
R2_ACCOUNT_ID=mock-account-id
R2_ACCESS_KEY_ID=mock-access-key
R2_SECRET_ACCESS_KEY=mock-secret-key
R2_BUCKET=manova-assets
R2_ENDPOINT=${ORBIT_S3_ENDPOINT}
EOF

WORKSPACE_DIR="${SANDBOX_DIR}/workspace"
export ORBIT_WORKSPACE="${WORKSPACE_DIR}"
mkdir -p "${WORKSPACE_DIR}"

cat > "${WORKSPACE_DIR}/workspace.yaml" <<EOF
version: "1"
workspace: "test-ws"
groups:
  test:
    path: ""
    repositories:
      - name: "test-repo"
        path: "test-repo"
        required: true
EOF

TEST_REPO="${WORKSPACE_DIR}/test-repo"
mkdir -p "${TEST_REPO}"
(
    cd "${TEST_REPO}"
    git init -q
    git config user.name "Test User"
    git config user.email "test@example.com"
    echo "# Test Repo" > README.md
    git add README.md
    git commit -q -m "initial commit"
)

log_step "Creating sample media asset..."
echo "Simulated high-resolution video asset content bytes 1234567890" > "${TEST_REPO}/hero-video.mp4"
original_hash=$(sha256sum "${TEST_REPO}/hero-video.mp4" | awk '{print $1}')

log_step "Adding asset with orbit assets add..."
add_out=$(cd "${TEST_REPO}" && run_orbit assets add hero-video.mp4)
assert_contains "${add_out}" "Added hero-video.mp4" "Asset added and hashed"

assert_file_exists "${TEST_REPO}/orbit-assets.yaml" "orbit-assets.yaml manifest created"
manifest_content=$(cat "${TEST_REPO}/orbit-assets.yaml")
assert_contains "${manifest_content}" "${original_hash}" "Manifest records correct SHA256 hash"

log_step "Checking asset status from workspace root..."
status_out=$(cd "${WORKSPACE_DIR}" && run_orbit assets status)
assert_contains "${status_out}" "ok" "Asset status reports ok"

log_step "Simulating missing local asset and pulling from mock store..."
rm -f "${TEST_REPO}/hero-video.mp4"

status_missing=$(cd "${WORKSPACE_DIR}" && run_orbit assets status 2>&1 || true)
assert_contains "${status_missing}" "missing" "Status detects missing local file"

pull_out=$(cd "${WORKSPACE_DIR}" && run_orbit assets pull --force)
assert_contains "${pull_out}" "Assets pulled" "Pull command completed"

assert_file_exists "${TEST_REPO}/hero-video.mp4" "Asset restored after pull"
restored_hash=$(sha256sum "${TEST_REPO}/hero-video.mp4" | awk '{print $1}')
assert_equals "${original_hash}" "${restored_hash}" "Restored file hash exactly matches original"

cleanup_test_sandbox
scenario_end

# ------------------------------------------------------------------------------
# AST-02: Bad Credentials & Missing R2 Config Error Handling
# ------------------------------------------------------------------------------
scenario_start "AST-02" "Asset Error Handling on Missing/Invalid Credentials"
setup_test_sandbox "ast-02"

WORKSPACE_DIR="${SANDBOX_DIR}/workspace"
export ORBIT_WORKSPACE="${WORKSPACE_DIR}"
mkdir -p "${WORKSPACE_DIR}"

cat > "${WORKSPACE_DIR}/workspace.yaml" <<EOF
version: "1"
workspace: "test-ws"
groups:
  test:
    path: ""
    repositories:
      - name: "test-repo"
        path: "test-repo"
        required: true
EOF

TEST_REPO="${WORKSPACE_DIR}/test-repo"
mkdir -p "${TEST_REPO}"
(
    cd "${TEST_REPO}"
    git init -q
    touch orbit-assets.yaml
)

log_step "Running assets pull with unconfigured R2 credentials..."
err_out=$(cd "${WORKSPACE_DIR}" && run_orbit assets pull 2>&1 || true)
assert_contains "${err_out}" "R2 credentials" "Clean error message on missing credentials"

cleanup_test_sandbox
scenario_end

print_scenario_summary "06_assets.sh"
