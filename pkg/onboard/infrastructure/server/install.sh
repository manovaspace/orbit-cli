#!/usr/bin/env bash
set -euo pipefail

REPO="manovaspace/orbit-cli"
DEFAULT_FALLBACK_VERSION="v0.23.0"
INSTALL_DIR="${ORBIT_INSTALL_DIR:-${HOME}/.local/bin}"
TMP_DIR=""
STAGE_FILE=""

cleanup() {
  if [ -n "$STAGE_FILE" ]; then
    rm -f "$STAGE_FILE"
  fi
  if [ -n "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

if [ "$(id -u)" -eq 0 ] && [ "${ORBIT_ALLOW_ROOT:-0}" != "1" ]; then
  echo "Error: Do not run install.sh as root; create a non-root user with sudo and run as that user (or set ORBIT_ALLOW_ROOT=1 for binary-only install)" >&2
  exit 1
fi

: "${ORBIT_YES:-}"
ARGS=()
HAS_ARGS=false
seen_dashdash=false
if [ $# -gt 0 ]; then
  for arg in "$@"; do
    if [ "$seen_dashdash" = true ]; then
      ARGS+=("$arg")
      HAS_ARGS=true
    elif [ "$arg" = "--" ]; then
      seen_dashdash=true
    elif [ "$arg" = "-y" ] || [ "$arg" = "--yes" ]; then
      :
    else
      ARGS+=("$arg")
      HAS_ARGS=true
    fi
  done
fi

if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  echo "Error: curl or wget is required to install Orbit" >&2
  exit 1
fi

download() {
  local url="$1"
  local dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    case "$url" in
      file://*) cp "${url#file://}" "$dest" ;;
      *) wget -qO "$dest" "$url" ;;
    esac
  else
    echo "Error: curl or wget is required to install Orbit" >&2
    exit 1
  fi
}

fetch_stdout() {
  local url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url"
  elif command -v wget >/dev/null 2>&1; then
    case "$url" in
      file://*) cat "${url#file://}" ;;
      *) wget -qO- "$url" ;;
    esac
  else
    echo "Error: curl or wget is required to install Orbit" >&2
    exit 1
  fi
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{ print $1 }'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{ print $1 }'
  else
    echo "Error: sha256sum or shasum is required to verify checksums" >&2
    exit 1
  fi
}

OS="${ORBIT_INSTALL_UNAME_S:-$(uname -s)}"
ARCH="${ORBIT_INSTALL_UNAME_M:-$(uname -m)}"
case "$OS" in
  Darwin)
    case "$ARCH" in
      arm64) ASSET="orbit-darwin-arm64" ;;
      x86_64|amd64) ASSET="orbit-darwin-amd64" ;;
      *)
        echo "Error: Unsupported macOS architecture '${ARCH}' (arm64 or amd64 required)" >&2
        exit 1
        ;;
    esac
    ;;
  Linux)
    case "$ARCH" in
      x86_64|amd64) ASSET="orbit-linux-amd64" ;;
      *)
        echo "Error: Unsupported Linux architecture '${ARCH}' (amd64 required)" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    echo "Error: Unsupported OS '${OS}' (Linux amd64 or macOS arm64/amd64 required)" >&2
    exit 1
    ;;
esac

if [ -n "${ORBIT_RELEASE_BASE_URL:-}" ]; then
  ORBIT_RELEASE_VERSION="${ORBIT_VERSION:-local}"
  BASE_URL="${ORBIT_RELEASE_BASE_URL%/}"
else
  if [ -z "${ORBIT_VERSION:-}" ]; then
    LATEST_TAG="$(fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | head -n 1 | cut -d '"' -f 4 || true)"
    ORBIT_RELEASE_VERSION="${LATEST_TAG:-$DEFAULT_FALLBACK_VERSION}"
  else
    ORBIT_RELEASE_VERSION="$ORBIT_VERSION"
  fi
  BASE_URL="https://github.com/${REPO}/releases/download/${ORBIT_RELEASE_VERSION}"
fi

install_orbit() {
  local asset="$1"
  local expected actual
  TMP_DIR="$(mktemp -d)"

  download "${BASE_URL}/${asset}" "${TMP_DIR}/${asset}"
  download "${BASE_URL}/checksums.txt" "${TMP_DIR}/checksums.txt"

  expected="$(awk -v f="$asset" '{ sub(/^\*/, "", $2) } $2 == f { print $1; exit }' "${TMP_DIR}/checksums.txt")"
  if [ -z "$expected" ]; then
    echo "Error: ${asset} is not listed in checksums.txt for ${ORBIT_RELEASE_VERSION}" >&2
    exit 1
  fi

  actual="$(sha256_file "${TMP_DIR}/${asset}")"
  if [ "$actual" != "$expected" ]; then
    echo "Error: SHA-256 mismatch for ${asset} (expected ${expected}, got ${actual})" >&2
    exit 1
  fi

  mkdir -p "$INSTALL_DIR"
  STAGE_FILE="${INSTALL_DIR}/.orbit.tmp.$$"
  cp "${TMP_DIR}/${asset}" "$STAGE_FILE"
  chmod 755 "$STAGE_FILE"
  mv -f "$STAGE_FILE" "${INSTALL_DIR}/orbit"
  STAGE_FILE=""
  if [ "$OS" = "Darwin" ]; then
    xattr -d com.apple.quarantine "${INSTALL_DIR}/orbit" 2>/dev/null || true
  fi
  ln -sf "orbit" "${INSTALL_DIR}/o"

  rm -rf "$TMP_DIR"
  TMP_DIR=""
}

install_orbit "$ASSET"

if [ "$HAS_ARGS" = true ]; then
  export PATH="${INSTALL_DIR}:${PATH}"
  if [ ! -t 0 ] && [ -r /dev/tty ] && (exec </dev/tty) 2>/dev/null; then
    exec "${INSTALL_DIR}/orbit" "${ARGS[@]}" </dev/tty
  else
    exec "${INSTALL_DIR}/orbit" "${ARGS[@]}"
  fi
fi

echo "Orbit ${ORBIT_RELEASE_VERSION} installed to ${INSTALL_DIR}/orbit"
echo "Next: o setup"
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    if [ "$INSTALL_DIR" = "${HOME}/.local/bin" ]; then
      echo "export PATH=\"\$HOME/.local/bin:\$PATH\""
    else
      echo "export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi
    ;;
esac
