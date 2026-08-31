package table

import (
	"os"
	"strconv"

	"golang.org/x/term"
)

const DefaultFallbackWidth = 120

func DetectTerminalWidth() int {
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			return w
		}
	}

	if cols := os.Getenv("COLUMNS"); cols != "" {
		if w, err := strconv.Atoi(cols); err == nil && w > 0 {
			return w
		}
	}

	return DefaultFallbackWidth
}
