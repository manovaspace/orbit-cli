#!/usr/bin/env bash
set -euo pipefail

REPO="manovaspace/orbit-cli"
INSTALL_DIR="${HOME}/.local/bin"

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
WARN="\033[33m"
ERR="\033[31m"
RESET="\033[0m"

if [ ! -t 1 ] || [ -n "${NO_COLOR:-}" ]; then
  BOLD=""
  BLUE=""
  CYAN=""
  GREEN=""
  GRAY=""
  WARN=""
  ERR=""
  RESET=""
fi

# Prompt confirmation helper reading strictly from /dev/tty
prompt_confirm() {
  local prompt_msg="$1"
  local default_val="${2:-y}"

  if [ "$YES_FLAG" = true ] || [ "${ORBIT_YES:-}" = "1" ]; then
    return 0
  fi

  if ! (exec 2>/dev/null 3</dev/tty); then
    echo "Error: Non-interactive environment detected without -y / --yes flag. Use -y or --yes to proceed non-interactively." >&2
    exit 1
  fi

  echo -ne "$prompt_msg"
  local response=""
  if read -r response </dev/tty; then
    if [ -z "$response" ]; then
      case "$default_val" in
        [yY]*) return 0 ;;
        *) return 1 ;;
      esac
    fi
    case "$response" in
      [yY]|[yY][eE][sS])
        return 0
        ;;
      *)
        return 1
        ;;
    esac
  fi
  return 1
}

if [ -z "${ORBIT_VERSION:-}" ]; then
  LATEST_TAG="$(curl -fsSL https://api.github.com/repos/${REPO}/releases/latest 2>/dev/null | grep '"tag_name":' | head -n 1 | cut -d '"' -f 4 || true)"
  if [ -n "$LATEST_TAG" ]; then
    ORBIT_RELEASE_VERSION="$LATEST_TAG"
  else
    ORBIT_RELEASE_VERSION="v0.9.1"
  fi
else
  ORBIT_RELEASE_VERSION="$ORBIT_VERSION"
fi

# Host constraints
OS="$(uname -s)"
if [ "$OS" != "Linux" ]; then
  echo "Error: Orbit requires Linux (Ubuntu 24.04 or 26.04 LTS, amd64)" >&2
  exit 1
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ;;
  *)
    echo "Error: Unsupported architecture '$ARCH' (amd64 required)" >&2
    exit 1
    ;;
esac

OS_ID=""
OS_VERSION_ID=""
if [ -f /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-}"
  OS_VERSION_ID="${VERSION_ID:-}"
elif [ -f /usr/lib/os-release ]; then
  # shellcheck disable=SC1091
  . /usr/lib/os-release
  OS_ID="${ID:-}"
  OS_VERSION_ID="${VERSION_ID:-}"
fi

if [ "$OS_ID" != "ubuntu" ]; then
  echo "Error: Orbit requires Linux (Ubuntu 24.04 or 26.04 LTS, amd64)" >&2
  exit 1
fi

case "$OS_VERSION_ID" in
  24.04*|26.04*) ;;
  *)
    echo "Error: Orbit requires Linux (Ubuntu 24.04 or 26.04 LTS, amd64)" >&2
    exit 1
    ;;
esac

if [ -f /proc/version ]; then
  PROC_VERSION="$(cat /proc/version)"
  if echo "$PROC_VERSION" | grep -qi microsoft; then
    IS_WSL2=false
    if echo "$PROC_VERSION" | grep -qi wsl2; then
      IS_WSL2=true
    fi
    if [ -e /run/WSL ]; then
      IS_WSL2=true
    fi
    if [ "$IS_WSL2" = false ]; then
      echo "Error: WSL1 is not supported (WSL2 required)" >&2
      exit 1
    fi
  fi
fi

# Check and auto-install curl/wget prerequisite
if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  echo -e "\n${WARN}⚠ Missing download prerequisite: curl or wget is required.${RESET}"
  if prompt_confirm "  ${BOLD}Install curl via sudo apt-get?${RESET} (Y/n) [y]: " "y"; then
    echo -e "  ${GRAY}Running: sudo apt-get update -qq && sudo apt-get install -y -qq curl${RESET}"
    sudo apt-get update -qq && sudo apt-get install -y -qq curl
  else
    echo "Error: curl or wget is required to install Orbit" >&2
    exit 1
  fi
fi

# Check and auto-install zsh prerequisite
if ! command -v zsh >/dev/null 2>&1; then
  echo -e "\n${WARN}⚠ Missing prerequisite: zsh is required for Orbit.${RESET}"
  if prompt_confirm "  ${BOLD}Install zsh via sudo apt-get?${RESET} (Y/n) [y]: " "y"; then
    echo -e "  ${GRAY}Running: sudo apt-get update -qq && sudo apt-get install -y -qq zsh${RESET}"
    sudo apt-get update -qq && sudo apt-get install -y -qq zsh
  else
    echo "Error: Orbit requires zsh as your login shell. Please run: sudo apt install -y zsh" >&2
    exit 1
  fi
fi

# Check and auto-configure zsh login shell
LOGIN_SHELL="$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f7 || true)"
SHELL_BASE="$(basename "${LOGIN_SHELL:-}")"
if [ "$SHELL_BASE" != "zsh" ]; then
  ZSH_PATH="$(command -v zsh 2>/dev/null || echo "/usr/bin/zsh")"
  echo -e "\n${WARN}⚠ Current login shell is not zsh (${LOGIN_SHELL:-unknown}).${RESET}"
  if prompt_confirm "  ${BOLD}Set zsh (${ZSH_PATH}) as your default login shell?${RESET} (Y/n) [y]: " "y"; then
    echo -e "  ${GRAY}Updating default login shell to ${ZSH_PATH}...${RESET}"
    chsh -s "$ZSH_PATH" 2>/dev/null || sudo chsh -s "$ZSH_PATH" "$(id -un)" 2>/dev/null || true
  else
    echo "Warning: Orbit requires zsh as your login shell. You can set it later with: chsh -s $(command -v zsh)" >&2
  fi
fi

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

echo -e "\n${BOLD}${BLUE}Orbit Platform CLI Installer${RESET}\n"
echo -e "  ${BOLD}Version:${RESET}      ${CYAN}${ORBIT_RELEASE_VERSION}${RESET}"
echo -e "  ${BOLD}Platform:${RESET}     linux / amd64"
echo -e "  ${BOLD}Destination:${RESET}  ${INSTALL_DIR}/orbit"
echo -e "  ${BOLD}Shortcuts:${RESET}    ${INSTALL_DIR}/o ${GRAY}(binary symlink)${RESET}\n"

# Interactive confirmation prompt for binary installation
if [ "$YES_FLAG" = false ] && [ "${ORBIT_YES:-}" != "1" ]; then
  if ! prompt_confirm "  ${BOLD}Do you want to proceed with the installation?${RESET} (y/N) [n]: " "n"; then
    echo -e "\n  ${GRAY}Installation cancelled. No changes were made to your system.${RESET}\n"
    exit 0
  fi
  echo ""
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

ORBIT_BIN="orbit-linux-amd64"
BASE_URL="https://github.com/${REPO}/releases/download/${ORBIT_RELEASE_VERSION}"

# Download binaries
echo -e "  ${GRAY}↓ Downloading release binaries from GitHub...${RESET}"
download "${BASE_URL}/${ORBIT_BIN}" "${TMP_DIR}/orbit"
chmod +x "${TMP_DIR}/orbit"

# Install binaries
mkdir -p "$INSTALL_DIR"
cp "${TMP_DIR}/orbit" "${INSTALL_DIR}/orbit"
chmod 755 "${INSTALL_DIR}/orbit"

# Create shortcut symlinks (o -> orbit)
ln -sf "orbit" "${INSTALL_DIR}/o" 2>/dev/null || true

# Clean up any legacy shell profile alias configuration from previous versions
CLEAN_PROFILES=(
  "${HOME}/.bashrc"
  "${HOME}/.bash_profile"
  "${HOME}/.zshrc"
  "${HOME}/.profile"
  "${HOME}/.config/fish/config.fish"
)

for rc in "${CLEAN_PROFILES[@]}"; do
  if [ -f "$rc" ] && [ -w "$rc" ]; then
    if grep -q -E "(# Orbit CLI shortcut|alias o=[\"']?orbit[\"']?)" "$rc" 2>/dev/null; then
      tmp_rc="$(mktemp)"
      grep -v -E "(# Orbit CLI shortcut|alias o=[\"']?orbit[\"']?)" "$rc" > "$tmp_rc" || true
      cat "$tmp_rc" > "$rc"
      rm -f "$tmp_rc"
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
  exec "${INSTALL_DIR}/orbit" "${NON_FLAG_ARGS[@]}"
fi

# Ensure ~/.local/bin is on PATH
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    if [ -f "${HOME}/.zshrc" ] && grep -q 'export PATH=.*\.local/bin' "${HOME}/.zshrc" 2>/dev/null; then
      :
    else
      echo -e "\n${WARN}⚠ ~/.local/bin is not currently in your \$PATH.${RESET}"
      if prompt_confirm "  ${BOLD}Add ~/.local/bin to PATH in ~/.zshrc?${RESET} (Y/n) [y]: " "y"; then
        mkdir -p "$HOME"
        touch "${HOME}/.zshrc"
        echo 'export PATH="$HOME/.local/bin:$PATH"' >> "${HOME}/.zshrc"
        echo -e "  ${GREEN}✔${RESET} Added export to ~/.zshrc"
      else
        echo "Error: ~/.local/bin is not on PATH (add export PATH=\"\$HOME/.local/bin:\$PATH\" to ~/.zshrc)" >&2
        exit 1
      fi
    fi
    ;;
esac

echo -e "  ${GREEN}✔${RESET} ${BOLD}Orbit ${ORBIT_RELEASE_VERSION} installed successfully!${RESET}\n"
echo -e "  ${BOLD}Installed to:${RESET}  ${INSTALL_DIR}/orbit"
echo -e "  ${BOLD}Commands:${RESET}      ${CYAN}orbit${RESET}, ${CYAN}o${RESET}\n"

echo -e "  ${BOLD}Get started:${RESET}"
echo -e "    ${CYAN}o admin init${RESET}  ${GRAY}# Platform owner (this machine vault)${RESET}"
echo -e "    ${CYAN}o invite create <email>${RESET}  ${GRAY}# Signed onboarding token${RESET}"
echo -e "    ${CYAN}o staff create --uid … --forward …${RESET}  ${GRAY}# Directory (needs orbit-staff)${RESET}"
echo -e "    ${CYAN}o onboard${RESET}     ${GRAY}# Invitee wizard (token from orbit invite)${RESET}"
echo -e "    ${CYAN}o doctor${RESET}      ${GRAY}# Verify system prerequisites${RESET}"
echo -e "    ${CYAN}o version${RESET}     ${GRAY}# Check installed version${RESET}\n"
