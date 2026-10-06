#!/usr/bin/env bash
# Check the agent diagnostic interface without running diagnostics or repairs.
set -euo pipefail

if (( $# > 1 )); then
  printf 'Usage: bash scripts/check-agent-cli.sh [orbit-binary]\n' >&2
  exit 2
fi

orbit_binary="${1:-orbit}"
if ! command -v -- "$orbit_binary" >/dev/null 2>&1; then
  printf 'Orbit executable unavailable: %s\n' "$orbit_binary" >&2
  exit 1
fi

if ! version_output="$("$orbit_binary" version)"; then
  printf 'Cannot inspect Orbit version: %s\n' "$orbit_binary" >&2
  exit 1
fi
if ! doctor_help="$("$orbit_binary" doctor --help)"; then
  printf 'Cannot inspect Orbit diagnostic help: %s\n' "$orbit_binary" >&2
  exit 1
fi

missing=()
for flag in --local --remote --accept-host-keys; do
  if ! awk -v flag="$flag" '$1 == flag { found = 1 } END { exit !found }' <<< "$doctor_help"; then
    missing+=("$flag")
  fi
done

printf '%s\n' "$version_output"
if (( ${#missing[@]} )); then
  printf 'Incompatible agent diagnostic interface; missing flags: %s\n' "${missing[*]}" >&2
  printf 'Validate an updated Orbit build before running workspace diagnostics.\n' >&2
  exit 1
fi
printf 'Agent diagnostic flags available: --local --remote --accept-host-keys\n'
printf 'This checks interface compatibility only; runtime health is unverified.\n'
