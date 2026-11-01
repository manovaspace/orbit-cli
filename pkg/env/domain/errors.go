package domain

import (
	"errors"
	"io/fs"
)

// isNotExist reports whether err says a path is missing, without importing os.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
