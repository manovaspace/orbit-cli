package toolchains

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

const (
	beginMarker = "# >>> orbit >>>"
	endMarker   = "# <<< orbit <<<"
)

// shellRCs maps a login shell's basename to the rc files that get the block, relative to home.
var shellRCs = map[string][]string{
	"zsh":  {".zshrc"},
	"bash": {".bashrc", ".profile"},
}

// PrimaryRC returns the rc file, relative to home, that the toolchains part writes first for the
// login shell named by shell, or "" when the shell is neither zsh nor bash.
func PrimaryRC(shell string) string {
	if shell == "" {
		return ""
	}
	rcs := shellRCs[filepath.Base(shell)]
	if len(rcs) == 0 {
		return ""
	}
	return rcs[0]
}

// Block returns the orbit block for platform, ending with a newline. Paths stay $HOME-relative so
// the block reads the same on every machine; home is accepted and not used.
func Block(platform, home string) string {
	lines := []string{beginMarker, "# managed by orbit setup; edits inside this block are replaced"}
	if platform == "darwin" {
		lines = append(lines,
			`if [ -x /opt/homebrew/bin/brew ]; then eval "$(/opt/homebrew/bin/brew shellenv)"; elif [ -x /usr/local/bin/brew ]; then eval "$(/usr/local/bin/brew shellenv)"; fi`,
			`if [ -n "$HOMEBREW_PREFIX" ]; then export PATH="$HOMEBREW_PREFIX/opt/node@24/bin:$HOMEBREW_PREFIX/opt/postgresql@18/bin:$PATH"; fi`,
		)
	}
	lines = append(lines,
		`export PATH="$HOME/.local/bin:$HOME/.bun/bin:$HOME/go/bin:$PATH"`,
		"export GOTOOLCHAIN=auto",
		endMarker,
	)
	return strings.Join(lines, "\n") + "\n"
}

// ApplyBlock returns existing with its orbit block replaced by block, or with block appended after
// exactly one blank line when there is no block. changed reports whether the result differs from
// existing. A begin marker without an end marker leaves existing unchanged and reports false.
func ApplyBlock(existing, block string) (string, bool) {
	if unterminated(existing) {
		return existing, false
	}
	block = strings.TrimSuffix(block, "\n") + "\n"
	blockLines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	lines := splitLines(existing)
	b := indexOf(lines, beginMarker, 0)

	var updated string
	switch {
	case b >= 0:
		e := indexOf(lines, endMarker, b+1)
		out := append(append(append([]string{}, lines[:b]...), blockLines...), lines[e+1:]...)
		updated = strings.Join(out, "\n") + "\n"
	case strings.TrimSpace(existing) == "":
		updated = block
	default:
		updated = strings.TrimRight(existing, "\n") + "\n\n" + block
	}
	return updated, updated != existing
}

// unterminated reports a begin marker with no end marker after it.
func unterminated(existing string) bool {
	lines := splitLines(existing)
	b := indexOf(lines, beginMarker, 0)
	return b >= 0 && indexOf(lines, endMarker, b+1) < 0
}

// splitLines splits text into lines without the final newline. Empty text has no lines.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// indexOf returns the index of the first line at or after from that equals marker, or -1.
func indexOf(lines []string, marker string, from int) int {
	for i := from; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == marker {
			return i
		}
	}
	return -1
}

// planShellRC plans the block into the rc files of the login shell. An unknown shell gets a warning
// that carries the block to paste by hand.
func (p part) planShellRC(s *engine.State, home string, plan *engine.Plan) error {
	name := p.getenv("SHELL")
	rcs, ok := shellRCs[filepath.Base(name)]
	if name == "" || !ok {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  "shell",
			Message:  fmt.Sprintf("login shell %q is not zsh or bash; paste the block into your shell rc file", name),
			Fix:      Block(s.Platform, home),
			Severity: engine.SeverityWarn,
		})
		return nil
	}
	block := Block(s.Platform, home)
	for _, rc := range rcs {
		if err := planRC(s, filepath.Join(home, rc), "~/"+rc, block, plan); err != nil {
			return err
		}
	}
	return nil
}

// planRC plans one action that writes block into path when the block would change. An rc file with
// an unterminated block is an error finding, never rewritten.
func planRC(s *engine.State, path, display, block string, plan *engine.Plan) error {
	existing, err := readRC(s.FS, path)
	if err != nil {
		return err
	}
	if unterminated(existing) {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  display,
			Message:  fmt.Sprintf("%s has an unterminated orbit block; fix it by hand", display),
			Severity: engine.SeverityError,
		})
		return nil
	}
	if _, changed := ApplyBlock(existing, block); !changed {
		return nil
	}
	fsys := s.FS
	plan.Actions = append(plan.Actions, engine.Action{
		Part:    partName,
		Subject: display,
		Summary: "Update " + display,
		Do:      func(context.Context) error { return writeBlock(fsys, path, block) },
	})
	return nil
}

// writeBlock re-reads path, applies block and writes the result with mode 0644. Plan checked the
// file already; a block found unterminated since then is an error, not a guess.
func writeBlock(fsys engine.FS, path, block string) error {
	existing, err := readRC(fsys, path)
	if err != nil {
		return err
	}
	if unterminated(existing) {
		return clierr.Failed(fmt.Sprintf("%s has an unterminated orbit block; fix it by hand", path))
	}
	updated, _ := ApplyBlock(existing, block)
	if err := fsys.WriteFile(path, []byte(updated), 0o644); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot write "+path))
	}
	return nil
}

// readRC returns the text of an rc file, or "" when it does not exist.
func readRC(fsys engine.FS, path string) (string, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", clierr.Wrap(err, clierr.Failed("cannot read "+path))
	}
	return string(data), nil
}
