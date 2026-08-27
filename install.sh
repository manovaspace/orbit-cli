#!/usr/bin/env bash
set -euo pipefail

REPO="manovaspace/orbit-cli"
INSTALL_DIR="${ORBIT_INSTALL_DIR:-/usr/local/bin}"

if [ -z "${ORBIT_VERSION:-}" ]; then
  LATEST_TAG="$(curl -fsSL https://api.github.com/repos/${REPO}/releases/latest 2>/dev/null | grep '"tag_name":' | head -n 1 | cut -d '"' -f 4 || true)"
  if [ -n "$LATEST_TAG" ]; then
    VERSION="$LATEST_TAG"
  else
    VERSION="v0.2.0"
  fi
else
  VERSION="$ORBIT_VERSION"
fi

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

EXT=""
if [ "$OS" = "windows" ]; then
  EXT=".exe"
fi

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

# Process flags
YES_FLAG=false
for arg in "$@"; do
  if [ "$arg" = "-y" ] || [ "$arg" = "--yes" ]; then
    YES_FLAG=true
  fi
done

# Colors
BOLD="\033[1m"
BLUE="\033[34m"
CYAN="\033[36m"
GREEN="\033[32m"
GRAY="\033[90m"
RESET="\033[0m"

if [ ! -t 1 ] || [ -n "${NO_COLOR:-}" ]; then
  BOLD=""
  BLUE=""
  CYAN=""
  GREEN=""
  GRAY=""
  RESET=""
fi

echo -e "\n${BOLD}${BLUE}Orbit Platform CLI Installer${RESET}\n"
echo -e "  ${BOLD}Version:${RESET}      ${CYAN}${VERSION}${RESET}"
echo -e "  ${BOLD}Platform:${RESET}     ${OS} / ${ARCH}"
echo -e "  ${BOLD}Destination:${RESET}  ${INSTALL_DIR}/orbit${EXT}"
echo -e "  ${BOLD}Shortcuts:${RESET}    ${INSTALL_DIR}/o${EXT} ${GRAY}(alias o=\"orbit\")${RESET}\n"

# Interactive confirmation prompt if terminal is available
if [ "$YES_FLAG" = false ] && [ "${ORBIT_YES:-}" != "1" ] && [ "${CI:-}" != "true" ] && [ -c /dev/tty ]; then
  echo -ne "  ${BOLD}Proceed with installation?${RESET} (Y/n) [Y]: "
  read -r CONFIRM </dev/tty || CONFIRM="y"
  if [[ "$CONFIRM" =~ ^[Nn] ]]; then
    echo -e "\n  ${GRAY}Installation cancelled by user.${RESET}\n"
    exit 0
  fi
  echo ""
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

ORBIT_BIN="orbit-${OS}-${ARCH}${EXT}"
MANOVA_BIN="manova-${OS}-${ARCH}${EXT}"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

# Download binaries
echo -e "  ${GRAY}↓ Downloading release binaries from GitHub...${RESET}"
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

# Direct execution if non-flag arguments passed
NON_FLAG_ARGS=()
for arg in "$@"; do
  if [ "$arg" != "-y" ] && [ "$arg" != "--yes" ]; then
    NON_FLAG_ARGS+=("$arg")
  fi
done

if [ ${#NON_FLAG_ARGS[@]} -gt 0 ]; then
  exec "${INSTALL_DIR}/orbit${EXT}" "${NON_FLAG_ARGS[@]}"
fi

echo -e "  ${GREEN}✔${RESET} ${BOLD}Orbit ${VERSION} installed successfully!${RESET}\n"
echo -e "  ${BOLD}Installed to:${RESET}  ${INSTALL_DIR}/orbit${EXT}"
echo -e "  ${BOLD}Commands:${RESET}      ${CYAN}orbit${RESET}, ${CYAN}o${RESET}, ${CYAN}manova${RESET}\n"
echo -e "  ${BOLD}Get started:${RESET}"
echo -e "    ${CYAN}o onboard${RESET}    ${GRAY}# Interactive onboarding wizard${RESET}"
echo -e "    ${CYAN}o doctor${RESET}     ${GRAY}# Verify system prerequisites${RESET}"
echo -e "    ${CYAN}o version${RESET}    ${GRAY}# Check installed version${RESET}\n"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo "  Note: Add $INSTALL_DIR to your PATH to run 'orbit':"
    echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
    echo ""
    ;;
esac
