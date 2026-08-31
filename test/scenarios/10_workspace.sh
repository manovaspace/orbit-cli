#!/usr/bin/env bash
# ==============================================================================
# Scenario Suite 10: Multi-Repo Workspace Orchestration (WKS-01)
# ==============================================================================

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

ensure_binaries_built

# ------------------------------------------------------------------------------
# WKS-01: Multi-Repo Workspace Lifecycle (Init, Status, Dirty Check, Sync, Repair)
# ------------------------------------------------------------------------------
scenario_start "WKS-01" "Multi-Repo Lifecycle: Init, Status, Sync & Repair"
setup_test_sandbox "wks-01"

WORKSPACE_DIR="${SANDBOX_DIR}/workspace"
REMOTES_DIR="${SANDBOX_DIR}/mock-remotes"
export ORBIT_WORKSPACE="${WORKSPACE_DIR}"

mkdir -p "${WORKSPACE_DIR}" "${REMOTES_DIR}"

log_step "Setting up mock bare git repositories..."
for repo in "repo-a" "repo-b"; do
    mkdir -p "${REMOTES_DIR}/${repo}.git"
    git init --bare -q "${REMOTES_DIR}/${repo}.git"
    
    # Push initial commit to bare repo
    tmp_init=$(mktemp -d)
    (
        cd "${tmp_init}"
        git init -q
        git config user.name "Test Committer"
        git config user.email "committer@example.com"
        echo "# ${repo}" > README.md
        git add README.md
        git commit -q -m "initial commit"
        git branch -M main
        git remote add origin "${REMOTES_DIR}/${repo}.git"
        git push -q -u origin main
    )
    rm -rf "${tmp_init}"
done

log_step "Creating workspace.yaml manifest pointing to mock remotes..."
cat > "${WORKSPACE_DIR}/workspace.yaml" <<EOF
version: "1"
workspace: "test-workspace"
remotes:
  local: "file://${REMOTES_DIR}"
groups:
  core:
    path: ""
    defaults:
      remote: "local"
      default_branch: "main"
    repositories:
      - name: "repo-a"
        path: "services/repo-a"
        repo: "repo-a.git"
        required: true
      - name: "repo-b"
        path: "services/repo-b"
        repo: "repo-b.git"
        required: true
EOF

log_step "Initializing workspace with orbit init..."
init_out=$(cd "${WORKSPACE_DIR}" && run_orbit init all)
assert_contains "${init_out}" "Cloning 2 repositories" "Init starts multi-repo clone"
assert_contains "${init_out}" "(cloned)" "Cloned confirmation displayed"

assert_file_exists "${WORKSPACE_DIR}/services/repo-a/.git" "repo-a cloned with .git"
assert_file_exists "${WORKSPACE_DIR}/services/repo-b/.git" "repo-b cloned with .git"

log_step "Checking workspace status (expect all clean)..."
status_clean=$(cd "${WORKSPACE_DIR}" && run_orbit status)
assert_contains "${status_clean}" "repo-a" "Status lists repo-a"
assert_contains "${status_clean}" "repo-b" "Status lists repo-b"
assert_contains "${status_clean}" "clean" "Status confirms clean working tree"

log_step "Modifying a file in repo-a (simulate dirty working tree)..."
echo "Uncommitted changes line" >> "${WORKSPACE_DIR}/services/repo-a/README.md"

status_dirty=$(cd "${WORKSPACE_DIR}" && run_orbit status)
assert_contains "${status_dirty}" "dirty" "Status flags modified uncommitted changes"

# Commit change in repo-a
(
    cd "${WORKSPACE_DIR}/services/repo-a"
    git config user.name "Test"
    git config user.email "test@example.com"
    git add README.md
    git commit -q -m "local feature commit"
)

log_step "Running orbit sync across workspace..."
sync_out=$(cd "${WORKSPACE_DIR}" && run_orbit sync all 2>&1 || true)
assert_contains "${sync_out}" "Syncing 2 repositories" "Sync executes for all targets"

log_step "Simulating gitless repository tree by removing .git from repo-b..."
rm -rf "${WORKSPACE_DIR}/services/repo-b/.git"
assert_file_not_exists "${WORKSPACE_DIR}/services/repo-b/.git" "repo-b .git deleted"

status_gitless=$(cd "${WORKSPACE_DIR}" && run_orbit status)
assert_contains "${status_gitless}" "gitless" "Status detects gitless repository"

log_step "Running orbit repair to re-attach .git without deleting files..."
repair_out=$(cd "${WORKSPACE_DIR}" && run_orbit repair all)
assert_contains "${repair_out}" "attached .git" "Repair re-attaches .git to gitless tree"

assert_file_exists "${WORKSPACE_DIR}/services/repo-b/.git" "repo-b .git re-attached successfully"

status_recovered=$(cd "${WORKSPACE_DIR}" && run_orbit status)
assert_contains "${status_recovered}" "clean" "Workspace restored to healthy clean state"

cleanup_test_sandbox
scenario_end

print_scenario_summary "10_workspace.sh"
