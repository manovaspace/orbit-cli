#!/usr/bin/env bash
set -euo pipefail

REPO="manovaspace/orbit-cli"
INSTALL_DIR="${HOME}/.local/bin"

# Process flags
YES_FLAG=false
CLI_ONLY_FLAG=false
for arg in "$@"; do
  if [ "$arg" = "-y" ] || [ "$arg" = "--yes" ]; then
    YES_FLAG=true
  elif [ "$arg" = "--cli-only" ]; then
    CLI_ONLY_FLAG=true
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

# Version comparison: returns 0 if $1 >= $2
version_ge() {
  printf '%s\n%s' "$2" "$1" | sort -V -C
}

# Resolve release version
if [ -z "${ORBIT_VERSION:-}" ]; then
  LATEST_TAG="$(curl -fsSL https://api.github.com/repos/${REPO}/releases/latest 2>/dev/null | grep '"tag_name":' | head -n 1 | cut -d '"' -f 4 || true)"
  if [ -n "$LATEST_TAG" ]; then
    ORBIT_RELEASE_VERSION="$LATEST_TAG"
  else
    ORBIT_RELEASE_VERSION="v0.9.4"
  fi
else
  ORBIT_RELEASE_VERSION="$ORBIT_VERSION"
fi

# Host validation
OS="$(uname -s)"
if [ "$OS" != "Linux" ]; then
  echo -e "${ERR}Error: Orbit requires Linux (Ubuntu 24.04 or 26.04 LTS, amd64)${RESET}" >&2
  exit 1
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ;;
  *)
    echo -e "${ERR}Error: Unsupported architecture '$ARCH' (amd64 required)${RESET}" >&2
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
  echo -e "${ERR}Error: Orbit requires Linux (Ubuntu 24.04 or 26.04 LTS, amd64)${RESET}" >&2
  exit 1
fi

case "$OS_VERSION_ID" in
  24.04*|26.04*) ;;
  *)
    echo -e "${ERR}Error: Orbit requires Linux (Ubuntu 24.04 or 26.04 LTS, amd64)${RESET}" >&2
    exit 1
    ;;
esac

if [ -f /proc/version ]; then
  PROC_VERSION="$(cat /proc/version)"
  if echo "$PROC_VERSION" | grep -qi microsoft; then
    IS_WSL2=false
    if echo "$PROC_VERSION" | grep -qi wsl2 || [ -e /run/WSL ]; then
      IS_WSL2=true
    fi
    if [ "$IS_WSL2" = false ]; then
      echo -e "${ERR}Error: WSL1 is not supported (WSL2 required)${RESET}" >&2
      exit 1
    fi
  fi
fi

# Helper: download
download() {
  local url="$1"
  local dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    echo -e "${ERR}Error: curl or wget is required to install Orbit${RESET}" >&2
    exit 1
  fi
}

# -----------------------------------------------------------------------------
# Pre-Flight System & Toolchain Inspection
# -----------------------------------------------------------------------------
MISSING_BASE_PKGS=()
PLANNED_TOOL_NAMES=()
PLANNED_CONFIG_NAMES=()

# 1. Base packages (curl, git, zsh, ca-certificates, unzip, tar, build-essential)
for pkg in curl git zsh ca-certificates unzip tar build-essential; do
  if ! dpkg -s "$pkg" >/dev/null 2>&1 && ! command -v "$pkg" >/dev/null 2>&1; then
    MISSING_BASE_PKGS+=("$pkg")
  fi
done

# 2. Git
GIT_LINE=""
if command -v git >/dev/null 2>&1; then
  GIT_VER="$(git version 2>/dev/null | grep -o '[0-9.]*' | head -n 1 || true)"
  GIT_LINE="  ${GREEN}✔${RESET}  Git                         v${GIT_VER} installed"
else
  GIT_LINE="  ${ERR}✖${RESET}  Git                         Missing (git package planned)"
  PLANNED_TOOL_NAMES+=("Git")
fi

# 3. Go compiler (>= 1.26.0)
NEED_GO=false
GO_LINE=""
if command -v go >/dev/null 2>&1; then
  RAW_GO="$(go version 2>/dev/null || true)"
  INSTALLED_GO="$(echo "$RAW_GO" | grep -o 'go[0-9.]*' | head -n 1 || true)"
  GO_VER="${INSTALLED_GO#go}"
  if [ -n "$GO_VER" ] && version_ge "$GO_VER" "1.26.0"; then
    GO_LINE="  ${GREEN}✔${RESET}  Go Compiler                 ${INSTALLED_GO} installed (>= 1.26.0 required)"
  else
    GO_LINE="  ${WARN}⚠${RESET}  Go Compiler                 ${INSTALLED_GO:-outdated} (upgrade to >= 1.26.0 planned)"
    NEED_GO=true
    PLANNED_TOOL_NAMES+=("Go compiler (latest stable >= 1.26)")
  fi
else
  GO_LINE="  ${ERR}✖${RESET}  Go Compiler                 Missing (Go 1.26+ planned)"
  NEED_GO=true
  PLANNED_TOOL_NAMES+=("Go compiler (latest stable >= 1.26)")
fi

# 4. Node.js (>= 24.0.0 LTS)
NEED_NODE=false
NODE_LINE=""
if command -v node >/dev/null 2>&1; then
  NODE_VER="$(node -v 2>/dev/null | tr -d 'v' || true)"
  NODE_MAJOR="${NODE_VER%%.*}"
  if [ -n "$NODE_MAJOR" ] && [ "$NODE_MAJOR" -ge 24 ] 2>/dev/null; then
    NODE_LINE="  ${GREEN}✔${RESET}  Node.js                     v${NODE_VER} installed (>= 24 required)"
  elif [ -n "$NODE_MAJOR" ] && [ "$NODE_MAJOR" -ge 20 ] 2>/dev/null; then
    NODE_LINE="  ${GREEN}✔${RESET}  Node.js                     v${NODE_VER} installed (LTS)"
  else
    NODE_LINE="  ${WARN}⚠${RESET}  Node.js                     v${NODE_VER:-outdated} (upgrade to Node 24 LTS planned)"
    NEED_NODE=true
    PLANNED_TOOL_NAMES+=("Node.js 24 LTS")
  fi
else
  NODE_LINE="  ${ERR}✖${RESET}  Node.js                     Missing (Node.js 24 LTS planned)"
  NEED_NODE=true
  PLANNED_TOOL_NAMES+=("Node.js 24 LTS")
fi

# 5. Bun (1.4.x)
NEED_BUN=false
BUN_LINE=""
if command -v bun >/dev/null 2>&1; then
  BUN_VER="$(bun -v 2>/dev/null || true)"
  BUN_LINE="  ${GREEN}✔${RESET}  Bun Runtime                 v${BUN_VER} installed"
else
  BUN_LINE="  ${ERR}✖${RESET}  Bun Runtime                 Missing (Bun 1.4.x planned)"
  NEED_BUN=true
  PLANNED_TOOL_NAMES+=("Bun JavaScript runtime")
fi

# 6. Docker & Docker Compose
NEED_DOCKER=false
DOCKER_LINE=""
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  DOCKER_VER="$(docker --version 2>/dev/null | grep -o '[0-9.]*' | head -n 1 || true)"
  DOCKER_LINE="  ${GREEN}✔${RESET}  Docker & Compose            v${DOCKER_VER} installed"
else
  DOCKER_LINE="  ${ERR}✖${RESET}  Docker & Compose            Missing (Docker Engine & Compose v2 planned)"
  NEED_DOCKER=true
  PLANNED_TOOL_NAMES+=("Docker Engine & Docker Compose v2")
fi

# 7. Caddy Reverse Proxy
NEED_CADDY=false
CADDY_LINE=""
if command -v caddy >/dev/null 2>&1; then
  CADDY_VER="$(caddy version 2>/dev/null | grep -o 'v[0-9.]*' | head -n 1 || echo "ready")"
  CADDY_LINE="  ${GREEN}✔${RESET}  Caddy Reverse Proxy         ${CADDY_VER} installed"
else
  CADDY_LINE="  ${ERR}✖${RESET}  Caddy Reverse Proxy         Missing (Caddy web server planned)"
  NEED_CADDY=true
  PLANNED_TOOL_NAMES+=("Caddy reverse proxy")
fi

# 8. Typst Compiler
NEED_TYPST=false
TYPST_LINE=""
if command -v typst >/dev/null 2>&1; then
  TYPST_VER="$(typst --version 2>/dev/null | grep -o '[0-9.]*' | head -n 1 || echo "ready")"
  TYPST_LINE="  ${GREEN}✔${RESET}  Typst Compiler              v${TYPST_VER} installed"
else
  TYPST_LINE="  ${ERR}✖${RESET}  Typst Compiler              Missing (Typst document CLI planned)"
  NEED_TYPST=true
  PLANNED_TOOL_NAMES+=("Typst compiler")
fi

# 9. Shell & Login Configuration
NEED_SHELL_SWITCH=false
LOGIN_SHELL="$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f7 || true)"
SHELL_BASE="$(basename "${LOGIN_SHELL:-}")"
SHELL_LINE=""
if [ "$SHELL_BASE" = "zsh" ]; then
  SHELL_LINE="  ${GREEN}✔${RESET}  Login Shell (zsh)           Default login shell configured (${LOGIN_SHELL})"
else
  SHELL_LINE="  ${WARN}⚠${RESET}  Login Shell (${SHELL_BASE:-unknown})          Switch default login shell to zsh planned"
  NEED_SHELL_SWITCH=true
  PLANNED_CONFIG_NAMES+=("Set default login shell to zsh")
fi

# 10. PATH exports
NEED_PATH_CONFIG=false
PATH_LINE=""
if [ -f "${HOME}/.zshrc" ] && grep -q 'export PATH=.*\.local/bin' "${HOME}/.zshrc" 2>/dev/null; then
  PATH_LINE="  ${GREEN}✔${RESET}  Environment PATH            ~/.local/bin configured in ~/.zshrc"
else
  PATH_LINE="  ${WARN}⚠${RESET}  Environment PATH            Export ~/.local/bin, /usr/local/go/bin, ~/.bun/bin in ~/.zshrc"
  NEED_PATH_CONFIG=true
  PLANNED_CONFIG_NAMES+=("Configure PATH in ~/.zshrc and ~/.bashrc")
fi

# -----------------------------------------------------------------------------
# Clean System & Action Preview Display
# -----------------------------------------------------------------------------
echo -e "\n${BOLD}${BLUE}Orbit Platform — Workstation Setup & Toolchain Installer${RESET}"
echo -e "Version: ${CYAN}${ORBIT_RELEASE_VERSION}${RESET} · Target: Linux / amd64 · Destination: ${INSTALL_DIR}/orbit\n"

echo -e "${BOLD}Detected Environment:${RESET}"
echo -e "$GIT_LINE"
echo -e "$GO_LINE"
echo -e "$NODE_LINE"
echo -e "$BUN_LINE"
echo -e "$DOCKER_LINE"
echo -e "$CADDY_LINE"
echo -e "$TYPST_LINE"
echo -e "$SHELL_LINE"
echo -e "$PATH_LINE\n"

HAS_SYSTEM_CHANGES=false
if [ ${#MISSING_BASE_PKGS[@]} -gt 0 ] || [ "$NEED_GO" = true ] || [ "$NEED_NODE" = true ] || \
   [ "$NEED_BUN" = true ] || [ "$NEED_DOCKER" = true ] || [ "$NEED_CADDY" = true ] || \
   [ "$NEED_TYPST" = true ] || [ "$NEED_SHELL_SWITCH" = true ] || [ "$NEED_PATH_CONFIG" = true ]; then
  HAS_SYSTEM_CHANGES=true
fi

echo -e "${BOLD}Planned Actions:${RESET}"
if [ ${#PLANNED_TOOL_NAMES[@]} -gt 0 ]; then
  TOOLS_JOINED="$(IFS=', '; echo "${PLANNED_TOOL_NAMES[*]}")"
  echo -e "  • Install missing toolchains: ${CYAN}${TOOLS_JOINED}${RESET}"
fi
if [ ${#PLANNED_CONFIG_NAMES[@]} -gt 0 ]; then
  for cfg in "${PLANNED_CONFIG_NAMES[@]}"; do
    echo -e "  • ${cfg}"
  done
fi
if [ "$NEED_DOCKER" = true ]; then
  echo -e "  • Add $(id -un) to docker group"
fi
echo -e "  • Install Orbit CLI ${ORBIT_RELEASE_VERSION} (${INSTALL_DIR}/orbit, shortcut: ${INSTALL_DIR}/o)\n"

# -----------------------------------------------------------------------------
# Single Unified Confirmation Prompt
# -----------------------------------------------------------------------------
PROCEED_FULL=true
if [ "$CLI_ONLY_FLAG" = true ]; then
  PROCEED_FULL=false
elif [ "$HAS_SYSTEM_CHANGES" = true ] && [ "$YES_FLAG" = false ] && [ "${ORBIT_YES:-}" != "1" ]; then
  if ! prompt_confirm "  ${BOLD}Ready to configure your system and install the required tools?${RESET} (Y/n) [y]: " "y"; then
    PROCEED_FULL=false
    if ! prompt_confirm "  ${BOLD}Install Orbit CLI only without system modifications?${RESET} (Y/n) [y]: " "y"; then
      echo -e "\n  ${GRAY}Installation cancelled. No changes were made to your system.${RESET}\n"
      exit 0
    fi
  fi
  echo ""
elif [ "$HAS_SYSTEM_CHANGES" = false ] && [ "$YES_FLAG" = false ] && [ "${ORBIT_YES:-}" != "1" ]; then
  if ! prompt_confirm "  ${BOLD}Install Orbit CLI ${ORBIT_RELEASE_VERSION}?${RESET} (Y/n) [y]: " "y"; then
    echo -e "\n  ${GRAY}Installation cancelled. No changes were made to your system.${RESET}\n"
    exit 0
  fi
  echo ""
fi

# -----------------------------------------------------------------------------
# Execution Phase
# -----------------------------------------------------------------------------
if [ "$PROCEED_FULL" = true ] && [ "$HAS_SYSTEM_CHANGES" = true ]; then
  echo -e "${BOLD}Configuring workstation dev environment...${RESET}\n"

  # 1. Base APT packages
  if [ ${#MISSING_BASE_PKGS[@]} -gt 0 ]; then
    echo -e "  ${GRAY}• Installing base system packages (${MISSING_BASE_PKGS[*]})...${RESET}"
    sudo apt-get update -qq
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${MISSING_BASE_PKGS[@]}"
    echo -e "    ${GREEN}✔${RESET} Base system packages installed"
  fi

  # 2. Zsh login shell
  if [ "$NEED_SHELL_SWITCH" = true ]; then
    ZSH_BIN="$(command -v zsh 2>/dev/null || echo "/usr/bin/zsh")"
    echo -e "  ${GRAY}• Setting default login shell to ${ZSH_BIN}...${RESET}"
    sudo chsh -s "$ZSH_BIN" "$(id -un)" 2>/dev/null || chsh -s "$ZSH_BIN" 2>/dev/null || true
    echo -e "    ${GREEN}✔${RESET} Login shell configured to zsh"
  fi

  # 3. Go compiler (>= 1.26)
  if [ "$NEED_GO" = true ]; then
    echo -e "  ${GRAY}• Installing Go compiler (latest stable)...${RESET}"
    GO_TARBALL_VER="$(curl -fsSL 'https://go.dev/dl/?mode=json' 2>/dev/null | grep -o '"version": "go[0-9.]*"' | head -n 1 | cut -d '"' -f 4 || echo "go1.26.0")"
    GO_FILE="${GO_TARBALL_VER}.linux-amd64.tar.gz"
    TMP_GO="$(mktemp -d)"
    download "https://go.dev/dl/${GO_FILE}" "${TMP_GO}/${GO_FILE}"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "${TMP_GO}/${GO_FILE}"
    sudo mkdir -p /etc/profile.d
    echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/orbit-go.sh >/dev/null
    sudo chmod 644 /etc/profile.d/orbit-go.sh
    rm -rf "$TMP_GO"
    export PATH="${PATH}:/usr/local/go/bin"
    echo -e "    ${GREEN}✔${RESET} Go compiler installed (${GO_TARBALL_VER})"
  fi

  # 4. Node.js 24 LTS
  if [ "$NEED_NODE" = true ]; then
    echo -e "  ${GRAY}• Installing Node.js 24 LTS via NodeSource...${RESET}"
    curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash - >/dev/null 2>&1 || true
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nodejs
    echo -e "    ${GREEN}✔${RESET} Node.js 24 LTS installed"
  fi

  # 5. Bun runtime
  if [ "$NEED_BUN" = true ]; then
    echo -e "  ${GRAY}• Installing Bun JavaScript runtime...${RESET}"
    curl -fsSL https://bun.sh/install | bash >/dev/null 2>&1 || true
    export PATH="${HOME}/.bun/bin:${PATH}"
    echo -e "    ${GREEN}✔${RESET} Bun runtime installed"
  fi

  # 6. Docker & Docker Compose
  if [ "$NEED_DOCKER" = true ]; then
    echo -e "  ${GRAY}• Installing Docker Engine and Docker Compose v2...${RESET}"
    sudo apt-get update -qq
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker.io docker-compose-v2 2>/dev/null || \
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker-ce docker-compose-plugin 2>/dev/null || true
    sudo systemctl enable --now docker 2>/dev/null || true
    sudo usermod -aG docker "$(id -un)" 2>/dev/null || true
    echo -e "    ${GREEN}✔${RESET} Docker & Docker Compose installed"
  fi

  # 7. Caddy reverse proxy
  if [ "$NEED_CADDY" = true ]; then
    echo -e "  ${GRAY}• Installing Caddy reverse proxy...${RESET}"
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg 2>/dev/null || true
    echo "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" | sudo tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null 2>&1 || true
    sudo apt-get update -qq
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq caddy 2>/dev/null || true
    echo -e "    ${GREEN}✔${RESET} Caddy reverse proxy installed"
  fi

  # 8. Typst document compiler
  if [ "$NEED_TYPST" = true ]; then
    echo -e "  ${GRAY}• Installing Typst document compiler...${RESET}"
    TMP_TYPST="$(mktemp -d)"
    TYPST_URL="https://github.com/typst/typst/releases/download/v0.13.0/typst-x86_64-unknown-linux-musl.tar.xz"
    if download "$TYPST_URL" "${TMP_TYPST}/typst.tar.xz" 2>/dev/null; then
      tar -xf "${TMP_TYPST}/typst.tar.xz" -C "${TMP_TYPST}"
      sudo cp "${TMP_TYPST}"/typst-*/typst /usr/local/bin/typst 2>/dev/null || cp "${TMP_TYPST}"/typst-*/typst "${INSTALL_DIR}/typst" 2>/dev/null || true
      sudo chmod 755 /usr/local/bin/typst 2>/dev/null || true
      echo -e "    ${GREEN}✔${RESET} Typst compiler installed"
    fi
    rm -rf "$TMP_TYPST"
  fi

  # 9. Configure ~/.zshrc & shell exports
  echo -e "  ${GRAY}• Configuring shell profiles and PATH exports...${RESET}"
  mkdir -p "$HOME"
  touch "${HOME}/.zshrc"
  PATH_LINE='export PATH="$HOME/.local/bin:/usr/local/go/bin:$HOME/.bun/bin:$PATH"'
  if ! grep -q 'export PATH=.*\.local/bin' "${HOME}/.zshrc" 2>/dev/null; then
    echo "$PATH_LINE" >> "${HOME}/.zshrc"
  fi
  if [ -f "${HOME}/.bashrc" ] && ! grep -q 'export PATH=.*\.local/bin' "${HOME}/.bashrc" 2>/dev/null; then
    echo "$PATH_LINE" >> "${HOME}/.bashrc"
  fi
  echo -e "    ${GREEN}✔${RESET} Shell environment configured"
fi

# 10. Install Orbit CLI Binary
echo -e "\n  ${GRAY}↓ Downloading Orbit CLI binaries from GitHub...${RESET}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

ORBIT_BIN="orbit-linux-amd64"
BASE_URL="https://github.com/${REPO}/releases/download/${ORBIT_RELEASE_VERSION}"

download "${BASE_URL}/${ORBIT_BIN}" "${TMP_DIR}/orbit"
chmod +x "${TMP_DIR}/orbit"

mkdir -p "$INSTALL_DIR"
cp "${TMP_DIR}/orbit" "${INSTALL_DIR}/orbit"
chmod 755 "${INSTALL_DIR}/orbit"
ln -sf "orbit" "${INSTALL_DIR}/o" 2>/dev/null || true

# Clean up legacy aliases
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

# Check PATH warning
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    if [ ! -f "${HOME}/.zshrc" ] || ! grep -q 'export PATH=.*\.local/bin' "${HOME}/.zshrc" 2>/dev/null; then
      mkdir -p "$HOME"
      touch "${HOME}/.zshrc"
      echo 'export PATH="$HOME/.local/bin:$PATH"' >> "${HOME}/.zshrc"
    fi
    ;;
esac

# Execute non-flag args directly if provided
NON_FLAG_ARGS=()
for arg in "$@"; do
  if [ "$arg" != "-y" ] && [ "$arg" != "--yes" ] && [ "$arg" != "--cli-only" ]; then
    NON_FLAG_ARGS+=("$arg")
  fi
done

if [ ${#NON_FLAG_ARGS[@]} -gt 0 ]; then
  exec "${INSTALL_DIR}/orbit" "${NON_FLAG_ARGS[@]}"
fi

echo -e "\n  ${GREEN}✔${RESET} ${BOLD}Orbit ${ORBIT_RELEASE_VERSION} installed successfully!${RESET}\n"
echo -e "  ${BOLD}Installed to:${RESET}  ${INSTALL_DIR}/orbit"
echo -e "  ${BOLD}Commands:${RESET}      ${CYAN}orbit${RESET}, ${CYAN}o${RESET}\n"

echo -e "  ${BOLD}Get started:${RESET}"
echo -e "    ${CYAN}o onboard${RESET}     ${GRAY}# Run onboarding wizard${RESET}"
echo -e "    ${CYAN}o doctor${RESET}      ${GRAY}# Verify system diagnostics${RESET}"
echo -e "    ${CYAN}o version${RESET}     ${GRAY}# Check installed version${RESET}\n"
