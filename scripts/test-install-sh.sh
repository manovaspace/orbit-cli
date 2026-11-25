#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_SH="${REPO_ROOT}/install.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

pass() {
  echo "PASS: $1"
}

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

RELEASE_DIR="${WORK_DIR}/release"
mkdir -p "$RELEASE_DIR"

for bin_name in orbit-linux-amd64 orbit-darwin-arm64; do
  cat > "${RELEASE_DIR}/${bin_name}" <<'EOF'
#!/usr/bin/env sh
echo "fake-orbit $*"
EOF
  chmod 755 "${RELEASE_DIR}/${bin_name}"
done

(
  cd "$RELEASE_DIR"
  printf '%s  %s\n' "$(sha256_file orbit-linux-amd64)" "orbit-linux-amd64" > checksums.txt
  printf '%s  %s\n' "$(sha256_file orbit-darwin-arm64)" "orbit-darwin-arm64" >> checksums.txt
)

# (a) Linux x86_64 with trailing args (`-- version`)
HOME_A="${WORK_DIR}/case-a/home"
DIR_A="${WORK_DIR}/case-a/bin"
mkdir -p "$HOME_A"
out_a="$(
  HOME="$HOME_A" \
  ORBIT_INSTALL_DIR="$DIR_A" \
  ORBIT_RELEASE_BASE_URL="file://${RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Linux \
  ORBIT_INSTALL_UNAME_M=x86_64 \
  bash "$INSTALL_SH" -- version </dev/null
)"
if [ "$out_a" = "fake-orbit version" ]; then
  pass "linux/amd64 -- version prints fake-orbit version"
else
  fail "linux/amd64 -- version got: ${out_a}"
fi

# (b) Tampered checksums.txt exits 1 and installs nothing new
BAD_RELEASE_DIR="${WORK_DIR}/release-bad"
mkdir -p "$BAD_RELEASE_DIR"
cp "${RELEASE_DIR}/orbit-linux-amd64" "${BAD_RELEASE_DIR}/orbit-linux-amd64"
echo "0000000000000000000000000000000000000000000000000000000000000000  orbit-linux-amd64" > "${BAD_RELEASE_DIR}/checksums.txt"

HOME_B="${WORK_DIR}/case-b/home"
DIR_B="${WORK_DIR}/case-b/bin"
mkdir -p "$HOME_B"
set +e
err_b="$(
  HOME="$HOME_B" \
  ORBIT_INSTALL_DIR="$DIR_B" \
  ORBIT_RELEASE_BASE_URL="file://${BAD_RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Linux \
  ORBIT_INSTALL_UNAME_M=x86_64 \
  bash "$INSTALL_SH" </dev/null 2>&1
)"
rc_b=$?
set -e
if [ "$rc_b" -ne 1 ]; then
  fail "tampered checksum expected exit 1, got ${rc_b} (${err_b})"
fi
if [ -e "${DIR_B}/orbit" ] || [ -e "${DIR_B}/o" ]; then
  fail "tampered checksum must not install orbit or o"
fi
pass "tampered checksums.txt exits 1 and installs nothing"

# (c) No args prints Next: o setup, PATH hint, and creates orbit + o link; -y/--yes/ORBIT_YES are silent no-ops
HOME_C="${WORK_DIR}/case-c/home"
DIR_C="${HOME_C}/.local/bin"
mkdir -p "$HOME_C"
out_c="$(
  HOME="$HOME_C" \
  ORBIT_INSTALL_DIR="$DIR_C" \
  ORBIT_RELEASE_BASE_URL="file://${RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Linux \
  ORBIT_INSTALL_UNAME_M=x86_64 \
  ORBIT_YES=1 \
  bash "$INSTALL_SH" -y --yes </dev/null
)"
echo "$out_c" | grep -q "Orbit local installed to ${DIR_C}/orbit" || fail "missing installed-to line: ${out_c}"
echo "$out_c" | grep -q "Next: o setup" || fail "missing Next: o setup line: ${out_c}"
echo "$out_c" | grep -Fq "export PATH=\"\$HOME/.local/bin:\$PATH\"" || fail "missing PATH hint: ${out_c}"
[ -x "${DIR_C}/orbit" ] || fail "orbit binary not executable at ${DIR_C}/orbit"
[ -L "${DIR_C}/o" ] || fail "o symlink missing at ${DIR_C}/o"
pass "no args prints Next: o setup, PATH hint, and creates orbit + o symlink"

# (d) Linux aarch64 exits 1
HOME_D="${WORK_DIR}/case-d/home"
DIR_D="${WORK_DIR}/case-d/bin"
mkdir -p "$HOME_D"
set +e
err_d="$(
  HOME="$HOME_D" \
  ORBIT_INSTALL_DIR="$DIR_D" \
  ORBIT_RELEASE_BASE_URL="file://${RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Linux \
  ORBIT_INSTALL_UNAME_M=aarch64 \
  bash "$INSTALL_SH" </dev/null 2>&1
)"
rc_d=$?
set -e
if [ "$rc_d" -ne 1 ]; then
  fail "linux/aarch64 expected exit 1, got ${rc_d} (${err_d})"
fi
pass "linux/aarch64 exits 1"

# (e) Darwin arm64 with `-- version`
HOME_E="${WORK_DIR}/case-e/home"
DIR_E="${WORK_DIR}/case-e/bin"
mkdir -p "$HOME_E"
out_e="$(
  HOME="$HOME_E" \
  ORBIT_INSTALL_DIR="$DIR_E" \
  ORBIT_RELEASE_BASE_URL="file://${RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Darwin \
  ORBIT_INSTALL_UNAME_M=arm64 \
  bash "$INSTALL_SH" -- version </dev/null
)"
if [ "$out_e" = "fake-orbit version" ]; then
  pass "darwin/arm64 -- version prints fake-orbit version"
else
  fail "darwin/arm64 -- version got: ${out_e}"
fi

# (f) wget fallback with file:// URL (no curl in PATH)
WGET_BIN="${WORK_DIR}/wget-only-bin"
mkdir -p "$WGET_BIN"
for tool in awk bash cat chmod cp cut env grep head id ln mkdir mktemp mv rm sh sha256sum shasum uname; do
  if tool_path="$(command -v "$tool" 2>/dev/null)"; then
    ln -sf "$tool_path" "${WGET_BIN}/${tool}"
  fi
done
cat > "${WGET_BIN}/wget" <<'EOF'
#!/usr/bin/env sh
echo "unexpected network wget call: $*" >&2
exit 1
EOF
chmod 755 "${WGET_BIN}/wget"

HOME_F="${WORK_DIR}/case-f/home"
DIR_F="${WORK_DIR}/case-f/bin"
mkdir -p "$HOME_F"
out_f="$(
  PATH="$WGET_BIN" \
  HOME="$HOME_F" \
  ORBIT_INSTALL_DIR="$DIR_F" \
  ORBIT_RELEASE_BASE_URL="file://${RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Linux \
  ORBIT_INSTALL_UNAME_M=x86_64 \
  bash "$INSTALL_SH" -- version </dev/null
)"
if [ "$out_f" = "fake-orbit version" ]; then
  pass "wget fallback handles file:// release URL without curl"
else
  fail "wget fallback got: ${out_f}"
fi

# (g) Root refusal and ORBIT_ALLOW_ROOT=1 override
ROOT_BIN="${WORK_DIR}/root-bin"
mkdir -p "$ROOT_BIN"
cat > "${ROOT_BIN}/id" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" = "-u" ]; then
  echo 0
  exit 0
fi
exec /usr/bin/id "$@"
EOF
chmod 755 "${ROOT_BIN}/id"

HOME_G="${WORK_DIR}/case-g/home"
DIR_G="${WORK_DIR}/case-g/bin"
mkdir -p "$HOME_G"
set +e
err_g="$(
  PATH="${ROOT_BIN}:${PATH}" \
  HOME="$HOME_G" \
  ORBIT_INSTALL_DIR="$DIR_G" \
  ORBIT_RELEASE_BASE_URL="file://${RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Linux \
  ORBIT_INSTALL_UNAME_M=x86_64 \
  bash "$INSTALL_SH" </dev/null 2>&1
)"
rc_g=$?
set -e
if [ "$rc_g" -ne 1 ]; then
  fail "root check expected exit 1, got ${rc_g}"
fi
echo "$err_g" | grep -q "Do not run install.sh as root" || fail "missing root refusal message: ${err_g}"

out_g2="$(
  PATH="${ROOT_BIN}:${PATH}" \
  HOME="$HOME_G" \
  ORBIT_INSTALL_DIR="$DIR_G" \
  ORBIT_RELEASE_BASE_URL="file://${RELEASE_DIR}" \
  ORBIT_INSTALL_UNAME_S=Linux \
  ORBIT_INSTALL_UNAME_M=x86_64 \
  ORBIT_ALLOW_ROOT=1 \
  bash "$INSTALL_SH" -- version </dev/null
)"
if [ "$out_g2" = "fake-orbit version" ]; then
  pass "root refused without ORBIT_ALLOW_ROOT=1 and allowed with ORBIT_ALLOW_ROOT=1"
else
  fail "ORBIT_ALLOW_ROOT=1 got: ${out_g2}"
fi
