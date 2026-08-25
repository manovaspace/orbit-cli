package changelog

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

// IsTerminal reports whether the given file descriptor is a real TTY.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// RunPager pipes content through the user's preferred pager ($PAGER, then
// 'less -R', then 'more') when stdout is a real TTY. Falls back to writing
// directly to w when no pager is available or stdout is a pipe.
func RunPager(w io.Writer, content string) error {
	out, ok := w.(*os.File)
	if !ok || !IsTerminal(out) {
		// Not a TTY (pipe, redirect, test buffer) — write directly.
		_, err := io.WriteString(w, content)
		return err
	}

	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}

	pagerArgs := []string{}
	if pager == "less" {
		// -R preserves ANSI colour codes; -F quits if content fits one screen
		pagerArgs = []string{"-R", "-F", "-X"}
	}

	cmd := exec.Command(pager, pagerArgs...)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Pager not found or failed — fall back to direct write.
		_, wErr := io.WriteString(w, content)
		return wErr
	}
	return nil
}
