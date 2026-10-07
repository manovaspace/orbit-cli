#!/usr/bin/env bash
# Build a source-only CLI checkout without the parent workspace or sibling repos.
set -euo pipefail

repository_root="$(realpath "$(dirname "${BASH_SOURCE[0]}")/..")"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/orbit-standalone.XXXXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT
mkdir "$test_root/orbit-cli"

git -C "$repository_root" ls-files -z -c -o --exclude-standard > "$test_root/source-files"
tar --null -C "$repository_root" -T "$test_root/source-files" -cf - |
  tar -C "$test_root/orbit-cli" -xf -

GOWORK=off go -C "$test_root/orbit-cli" build -mod=readonly -o "$test_root/orbit" ./cmd/orbit
GOWORK=off CGO_ENABLED=0 GOOS=linux go -C "$test_root/orbit-cli" build -mod=readonly -trimpath -o "$test_root/orbit-server" ./cmd/orbit-server
cmp "$repository_root/go.mod" "$test_root/orbit-cli/go.mod"
cmp "$repository_root/go.sum" "$test_root/orbit-cli/go.sum"
printf 'Standalone CLI and Linux server builds passed; module files unchanged.\n'
