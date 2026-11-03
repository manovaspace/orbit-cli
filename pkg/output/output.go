// Package output renders the JSON envelope every machine-readable command emits.
package output

import (
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// written records whether Write has emitted an envelope since the last
// ResetWritten. The root command uses it to avoid writing a second envelope
// after a command has already reported its own failure.
var written atomic.Bool

// Written reports whether Write has emitted an envelope since the last
// ResetWritten.
func Written() bool {
	return written.Load()
}

// ResetWritten clears the written flag. The root command calls it before every
// run; tests call it to isolate runs.
func ResetWritten() {
	written.Store(false)
}

// SchemaVersion is bumped when the envelope shape changes incompatibly.
const SchemaVersion = 1

// ErrorBody is the machine-readable form of a failed command.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// Envelope wraps a command result in a stable, versioned shape.
type Envelope struct {
	SchemaVersion int        `json:"schemaVersion"`
	OK            bool       `json:"ok"`
	Data          any        `json:"data,omitempty"`
	Error         *ErrorBody `json:"error,omitempty"`
}

// Success builds an envelope for a successful command.
func Success(data any) Envelope {
	return Envelope{SchemaVersion: SchemaVersion, OK: true, Data: data}
}

// Failure builds an envelope for a failed command, taking the code and hint
// from a clierr.Error when the error chain contains one.
func Failure(err error) Envelope {
	body := &ErrorBody{Code: clierr.Code(err)}
	if err != nil {
		body.Message = err.Error()
	}
	var c *clierr.Error
	if errors.As(err, &c) {
		body.Hint = c.Hint
	}
	return Envelope{SchemaVersion: SchemaVersion, OK: false, Error: body}
}

// Write encodes e as a single line of JSON followed by a newline.
func Write(w io.Writer, e Envelope) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err = w.Write(b); err != nil {
		return err
	}
	written.Store(true)
	return nil
}
