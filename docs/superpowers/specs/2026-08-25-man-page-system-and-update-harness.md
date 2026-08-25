# Unix Man Page System & Release Update Harness — Spec

**Goal:** Provide complete, automated Unix man pages (section 1) for `manova`, `m`, and all subcommands, with automated installation on bootstrap, removal on uninstall, and automatic refresh on every self-update and release.

---

## 1. Man Page Architecture

### Generator Engine (`pkg/manpage`)
Using `github.com/spf13/cobra/doc.GenManTree`:
- Generates section 1 man pages (`.1` files) for the root `manova` command and all child subcommands.
- Configures header metadata:
  - `Title`: `MANOVA`
  - `Section`: `1`
  - `Manual`: `Manova CLI Developer Reference`
  - `Source`: `Manova Orbit Toolkit`
- Generates `m.1` alias link pointing to `manova.1` so `man m` works identically to `man manova`.

### Directory Target Resolution
- **System-wide path (Root / Sudo):** `/usr/local/share/man/man1`
- **User-local path (Unprivileged):** `~/.local/share/man/man1` (and ensure `~/.local/share/man` is accessible to `man` or added to user's environment).
- After writing files, runs `mandb -q` (or `makewhatis`) if available in PATH to refresh system index.

---

## 2. Integration Points & Update Harness

### A. Environment Bootstrap (`manova init --bootstrap` & `manova onboard`)
During environment initialization:
- `manpage.InstallManPages(rootCmd)` is executed.
- Installs man pages into system or user man directory.

### B. Post-Update Migration Harness (`pkg/migrate/post_update.go`)
- Adds migration `004_refresh_man_pages`:
- When `manova self-update` installs a new version, `RunPostUpdateMigrations()` executes this hook, automatically generating and replacing installed man pages with updated command flags and documentation.

### C. Uninstaller (`manova uninstall`)
- Cleans up generated `manova*.1` and `m.1` files from `/usr/local/share/man/man1` and `~/.local/share/man/man1`.

### D. Standalone Subcommands (`cmd/manova/doc.go` / `manova doc man`)
- `manova doc man [output-dir]`: Exports generated man pages to any directory.
- `manova doc markdown [output-dir]`: Exports markdown docs.

### E. Build & Release Harness (`scripts/build-release.sh`)
- Generates a `dist/man/` directory containing all `.1` files alongside release binaries.

---

## 3. Files Touched

| File | Change |
|---|---|
| `orbit/orbit-cli/pkg/manpage/manpage.go` | New package: Generate, Install, Uninstall, Directory resolution |
| `orbit/orbit-cli/pkg/manpage/manpage_test.go` | Unit tests for man page generation and installation |
| `orbit/orbit-cli/pkg/migrate/post_update.go` | Add migration `004_refresh_man_pages` |
| `orbit/orbit-cli/cmd/manova/doc.go` | New command `manova doc` (`man`, `markdown`) |
| `orbit/orbit-cli/cmd/manova/init.go` | Call `manpage.InstallManPages` on bootstrap |
| `orbit/orbit-cli/cmd/manova/onboard.go` | Call `manpage.InstallManPages` during setup |
| `orbit/orbit-cli/cmd/manova/uninstall.go` | Clean up man pages on uninstall |
| `orbit/orbit-cli/scripts/build-release.sh` | Generate man pages into release package |
