#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-v0.1.4}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo "none")"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"

echo "Building manova CLI ${VERSION} (commit: ${COMMIT}, date: ${DATE})..."

mkdir -p dist

GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o dist/manova-linux-amd64 ./cmd/manova
GOOS=linux GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o dist/manova-linux-arm64 ./cmd/manova

# Generate Unix Man Pages
mkdir -p dist/man/man1
./dist/manova-linux-amd64 doc man dist/man/man1
tar -czf dist/manova-manpages.tar.gz -C dist/man man1

echo "Build complete. Artifacts in dist/:"
ls -lh dist/
