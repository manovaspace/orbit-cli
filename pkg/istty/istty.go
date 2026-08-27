// Package istty provides terminal detection utilities.
package istty

import (
	"os"

	"golang.org/x/crypto/ssh/terminal"
)

// IsInteractiveSession reports whether stdin is connected to a terminal.
func IsInteractiveSession() bool {
	fd := int(os.Stdin.Fd())
	return terminal.IsTerminal(fd)
}
