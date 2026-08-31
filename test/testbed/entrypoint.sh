#!/usr/bin/env bash
set -e

# Initialize testbed environment and configuration directories
mkdir -p "${HOME}/.orbit" "${HOME}/.local/state/orbit" "${HOME}/.config" "${HOME}/go/bin"

# Mark workspace safe for git across different UID/GIDs
git config --global --add safe.directory '*' 2>/dev/null || true

# If running as root, fix permissions for workspace and home
if [ "$(id -u)" = "0" ]; then
    chown -R vscode:vscode "${HOME}" /workspace 2>/dev/null || true
fi

# Dynamically populate /etc/hosts with edge hostname aliases if ORBIT_EDGE_HOST is provided
if [ -n "${ORBIT_EDGE_HOST:-}" ]; then
    EDGE_IP=$(getent hosts "${ORBIT_EDGE_HOST}" | awk '{ print $1 }' | head -n1)
    if [ -n "${EDGE_IP}" ]; then
        for alias in orbit.dev.manova.space staff.dev.manova.space git.dev.manova.space assets.dev.manova.space; do
            if ! grep -q "[[:space:]]${alias}" /etc/hosts; then
                if [ "$(id -u)" = "0" ]; then
                    echo "${EDGE_IP} ${alias}" >> /etc/hosts
                else
                    sudo sh -c "echo '${EDGE_IP} ${alias}' >> /etc/hosts" 2>/dev/null || true
                fi
            fi
        done
    fi
fi

# If no arguments provided, keep container running
if [ $# -eq 0 ]; then
    exec sleep infinity
else
    exec "$@"
fi
