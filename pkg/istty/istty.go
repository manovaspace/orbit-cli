// Package istty provides terminal detection utilities.
package istty

import (
	"os"

	"golang.org/x/term"
)

// IsInteractiveSession reports whether stdin is connected to a terminal.
func IsInteractiveSession() bool {
	fd := int(os.Stdin.Fd())
	return term.IsTerminal(fd)
}
