# Orbit CLI UNIX Man Page Installation & Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically generate and install Orbit CLI UNIX man pages into `~/.local/share/man/man1` on installation with `mandb` indexing, and clean them up during uninstallation.

**Architecture:** Use the built-in `orbit doc -f man` generator within `install.sh` to self-generate roff man pages into standard user `~/.local/share/man/man1` with resilient `mandb` refreshing, and add matching cleanup logic to `cmd/orbit/uninstall.go`.

**Tech Stack:** Bash, Go (Cobra, stdlib), UNIX mandb / man-db.

## Global Constraints
- Destination: `~/.local/share/man/man1` (user-level, non-root, standard in Debian/Ubuntu `manpath`).
- Parity: Both `install.sh` and `pkg/onboard/install.sh` must be kept in sync.
- Resilience: Non-zero exit code or missing `mandb` must never abort installation or uninstallation.
- Uninstallation: `orbit uninstall` removes only `orbit*.1` files, leaving other man pages untouched.

---

### Task 1: Uninstaller Man Page Cleanup & Unit Test

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/uninstall.go`
- Create: `orbit/orbit-cli/cmd/orbit/uninstall_test.go`

**Interfaces:**
- Consumes: `removeFileElevated(path string) error`
- Produces: `cleanManPages(home string, out io.Writer) int` helper or inline cleanup in `newUninstallCmd()`

- [ ] **Step 1: Write the failing test for man page cleanup**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanManPages(t *testing.T) {
	tmpDir := t.TempDir()
	man1Dir := filepath.Join(tmpDir, ".local", "share", "man", "man1")
	if err := os.MkdirAll(man1Dir, 0755); err != nil {
		t.Fatalf("failed to create mock man directory: %v", err)
	}

	// Create mock orbit man pages and unrelated man page
	orbit1 := filepath.Join(man1Dir, "orbit.1")
	orbitStaff1 := filepath.Join(man1Dir, "orbit-staff.1")
	other1 := filepath.Join(man1Dir, "other.1")

	_ = os.WriteFile(orbit1, []byte(".TH ORBIT 1"), 0644)
	_ = os.WriteFile(orbitStaff1, []byte(".TH ORBIT-STAFF 1"), 0644)
	_ = os.WriteFile(other1, []byte(".TH OTHER 1"), 0644)

	var buf bytes.Buffer
	removed := cleanManPagesWithHome(tmpDir, &buf)

	if removed != 2 {
		t.Errorf("expected 2 man pages removed, got %d", removed)
	}

	if _, err := os.Stat(orbit1); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed", orbit1)
	}
	if _, err := os.Stat(orbitStaff1); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed", orbitStaff1)
	}
	if _, err := os.Stat(other1); os.IsNotExist(err) {
		t.Errorf("expected %s to remain intact", other1)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./cmd/orbit -run TestCleanManPages`
Expected: FAIL with undefined `cleanManPagesWithHome`

- [ ] **Step 3: Implement man page cleanup in `cmd/orbit/uninstall.go`**

Implement `cleanManPagesWithHome(home string, out io.Writer) int` and call it inside `newUninstallCmd()`:

```go
func cleanManPagesWithHome(home string, out io.Writer) int {
	removedCount := 0
	manDirs := []string{
		filepath.Join(home, ".local", "share", "man", "man1"),
	}
	for _, md := range manDirs {
		matches, err := filepath.Glob(filepath.Join(md, "orbit*.1"))
		if err == nil {
			for _, mf := range matches {
				if err := removeFileElevated(mf); err == nil {
					fmt.Fprintf(out, "  %s  Removed man page: %s\n", iconOK, subtleStyle.Render(mf))
					removedCount++
				}
			}
		}
	}
	if _, err := exec.LookPath("mandb"); err == nil {
		_ = exec.Command("mandb", "-q").Run()
	}
	return removedCount
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./cmd/orbit -run TestCleanManPages`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/uninstall.go cmd/orbit/uninstall_test.go
git -C orbit/orbit-cli commit -m "feat(uninstall): clean installed man pages and refresh mandb"
```

---

### Task 2: Installer Script Man Page Generation

**Files:**
- Modify: `orbit/orbit-cli/install.sh`
- Modify: `orbit/orbit-cli/pkg/onboard/install.sh`

**Interfaces:**
- Consumes: `${INSTALL_DIR}/orbit doc -f man -o "${HOME}/.local/share/man/man1"`

- [ ] **Step 1: Add step 11 to `install.sh`**

In `orbit/orbit-cli/install.sh`:
```bash
# 11. Install UNIX Man Pages
echo -e "  ${GRAY}• Installing UNIX man pages...${RESET}"
MAN1_DIR="${HOME}/.local/share/man/man1"
mkdir -p "$MAN1_DIR"
if "${INSTALL_DIR}/orbit" doc -f man -o "$MAN1_DIR" >/dev/null 2>&1; then
  if command -v mandb >/dev/null 2>&1; then
    mandb -q "$MAN1_DIR" >/dev/null 2>&1 || mandb -q >/dev/null 2>&1 || true
  fi
  echo -e "    ${GREEN}✔${RESET} UNIX man pages installed (${MAN1_DIR})"
fi
```

- [ ] **Step 2: Sync change to `pkg/onboard/install.sh`**

Ensure `orbit/orbit-cli/pkg/onboard/install.sh` has the exact same block.

- [ ] **Step 3: Test bash syntax of both install scripts**

Run:
```bash
bash -n orbit/orbit-cli/install.sh
bash -n orbit/orbit-cli/pkg/onboard/install.sh
```
Expected: Exit code 0 (no syntax errors).

- [ ] **Step 4: Commit**

```bash
git -C orbit/orbit-cli add install.sh pkg/onboard/install.sh
git -C orbit/orbit-cli commit -m "feat(install): automatically generate UNIX man pages and index mandb"
```

---

### Task 3: Full Test Suite & Verification

**Files:**
- Test all tests in `orbit/orbit-cli`

- [ ] **Step 1: Run all tests in `orbit/orbit-cli`**

Run: `go test -v ./...` in `orbit/orbit-cli`
Expected: ALL PASS

- [ ] **Step 2: Verify `man -w orbit` and `man -w orbit-staff-list`**

Run: `man -w orbit && man -w orbit-staff-list`
Expected: Output path pointing to `~/.local/share/man/man1/orbit.1` and `orbit-staff-list.1`
