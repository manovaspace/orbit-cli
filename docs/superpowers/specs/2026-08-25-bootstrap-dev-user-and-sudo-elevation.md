# Developer Environment Bootstrap, Dedicated 'dev' User & Sudo Elevation — Spec

**Goal:** Ensure that running `curl get.manova.space | bash` immediately transitions the developer into a fully configured, active Manova development environment (Zsh + Oh My Zsh + `m` alias + worker timer), handles dedicated non-root `dev` user creation when run as root, and uses a single `sudo -v` elevation warmup for non-root users.

## 1. Requirements

1. **Seamless CLI Handoff from Installer:**
   - `get.sh` downloads the binary and immediately transfers execution to `manova`:
     - With arguments: `exec "$TARGET_BIN" onboard "$@"`
     - With token: `exec "$TARGET_BIN" onboard --token "$INVITE_TOKEN"`
     - Without token (or skipped): `exec "$TARGET_BIN" init --bootstrap`

2. **Dedicated Developer User (`dev`) on Root Execution:**
   - If `os.Geteuid() == 0` (running on fresh VPS as root):
     - Prompt: `Running as root is not recommended for developer workspaces. Would you like to create a dedicated developer user with sudo privileges? [Y/n]` (default: Yes).
     - Username prompt: default `dev`.
     - Creates user `useradd -m -s /usr/bin/zsh -G sudo,docker dev`.
     - Sets up passwordless sudo rule in `/etc/sudoers.d/90-manova-dev` (`dev ALL=(ALL) NOPASSWD:ALL`).
     - Copies `/root/.ssh/authorized_keys` to `/home/dev/.ssh/authorized_keys` with proper `chown` & `0600`/`0700` permissions.
     - Sets up `/home/dev/.zshrc` with Oh My Zsh, completions, and `alias m="manova"`.

3. **Single Sudo Password Prompt (Warmup):**
   - For non-root users, before running privileged commands (`apt`, `chsh`), call `sudo -v` once. This prompts the user for their password a single time and refreshes sudo's credential timestamp cache for the entire setup duration.

4. **Environment Initialization (`manova init --bootstrap`):**
   - Installs Zsh and Oh My Zsh if missing.
   - Sets default login shell to Zsh (`/usr/bin/zsh`).
   - Configures `~/.zshrc` with Oh My Zsh template, `alias m="manova"`, and Zsh completion.
   - Starts the background update worker daemon (`manova-worker.timer` or detached).
   - Reports clear onboarding guidance.
