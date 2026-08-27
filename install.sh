#!/usr/bin/env bash
set -euo pipefail

REPO="manovaspace/orbit-cli"
VERSION="${ORBIT_VERSION:-v0.1.0}"
INSTALL_DIR="${ORBIT_INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Linux*)   OS="linux" ;;
  Darwin*)  OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *)
    echo "Error: Unsupported operating system '$OS'" >&2
    exit 1
    ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture '$ARCH'" >&2
    exit 1
    ;;
esac

# Download helper
download() {
  local url="$1"
  local dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    echo "Error: curl or wget is required to install Orbit" >&2
    exit 1
  fi
}

# Determine write permission for destination directory
check_write_permission() {
  local dir="$1"
  while [ ! -d "$dir" ]; do
    dir="$(dirname "$dir")"
    if [ "$dir" = "/" ] || [ "$dir" = "." ]; then
      break
    fi
  done
  [ -w "$dir" ]
}

USE_SUDO=false
if [ "$(id -u)" -ne 0 ]; then
  if ! check_write_permission "$INSTALL_DIR"; then
    if command -v sudo >/dev/null 2>&1; then
      USE_SUDO=true
    else
      INSTALL_DIR="${HOME}/.local/bin"
    fi
  fi
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

EXT=""
if [ "$OS" = "windows" ]; then
  EXT=".exe"
fi

ORBIT_BIN="orbit-${OS}-${ARCH}${EXT}"
MANOVA_BIN="manova-${OS}-${ARCH}${EXT}"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

# Download binaries silently
download "${BASE_URL}/${ORBIT_BIN}" "${TMP_DIR}/orbit${EXT}"
chmod +x "${TMP_DIR}/orbit${EXT}"

download "${BASE_URL}/${MANOVA_BIN}" "${TMP_DIR}/manova${EXT}" 2>/dev/null || true
if [ -f "${TMP_DIR}/manova${EXT}" ]; then
  chmod +x "${TMP_DIR}/manova${EXT}"
fi

# Install binaries
if [ "$USE_SUDO" = true ]; then
  sudo mkdir -p "$INSTALL_DIR"
  sudo cp "${TMP_DIR}/orbit${EXT}" "${INSTALL_DIR}/orbit${EXT}"
  sudo chmod 755 "${INSTALL_DIR}/orbit${EXT}"
  if [ -f "${TMP_DIR}/manova${EXT}" ]; then
    sudo cp "${TMP_DIR}/manova${EXT}" "${INSTALL_DIR}/manova${EXT}"
    sudo chmod 755 "${INSTALL_DIR}/manova${EXT}"
  fi
else
  mkdir -p "$INSTALL_DIR"
  cp "${TMP_DIR}/orbit${EXT}" "${INSTALL_DIR}/orbit${EXT}"
  chmod 755 "${INSTALL_DIR}/orbit${EXT}"
  if [ -f "${TMP_DIR}/manova${EXT}" ]; then
    cp "${TMP_DIR}/manova${EXT}" "${INSTALL_DIR}/manova${EXT}"
    chmod 755 "${INSTALL_DIR}/manova${EXT}"
  fi
fi

# Create shortcut symlinks (o -> orbit, m -> manova)
if [ "$USE_SUDO" = true ]; then
  sudo ln -sf "orbit${EXT}" "${INSTALL_DIR}/o${EXT}" 2>/dev/null || true
  sudo ln -sf "orbit${EXT}" "${INSTALL_DIR}/m${EXT}" 2>/dev/null || true
else
  ln -sf "orbit${EXT}" "${INSTALL_DIR}/o${EXT}" 2>/dev/null || true
  ln -sf "orbit${EXT}" "${INSTALL_DIR}/m${EXT}" 2>/dev/null || true
fi

# Configure shell shortcut alias in ~/.bashrc and ~/.zshrc
for rc in "${HOME}/.bashrc" "${HOME}/.zshrc"; do
  if [ -f "$rc" ] && [ -w "$rc" ]; then
    if ! grep -q "alias o=" "$rc" 2>/dev/null; then
      echo -e "\n# Orbit CLI shortcut\nalias o=\"orbit\"" >> "$rc"
    fi
  fi
done

# Direct execution if arguments passed
if [ $# -gt 0 ]; then
  exec "${INSTALL_DIR}/orbit${EXT}" "$@"
fi

# Minimalist confirmation
GREEN="\033[32m"
RESET="\033[0m"

if [ ! -t 1 ] || [ -n "${NO_COLOR:-}" ]; then
  GREEN=""
  RESET=""
fi

echo ""
echo -e "  ${GREEN}✔${RESET} orbit ${VERSION} installed (${INSTALL_DIR}/orbit${EXT})"
echo ""
echo "  Get started:"
echo "    orbit onboard"
echo ""

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo "  Note: Add $INSTALL_DIR to your PATH to run 'orbit':"
    echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
    echo ""
    ;;
esac
