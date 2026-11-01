// Package clierr carries the exit code and stable machine code of a CLI error.
package clierr

import "errors"

// Exit codes shared by every orbit command.
const (
	ExitOK         = 0
	ExitFailed     = 1 // findings, or the operation failed
	ExitUnverified = 2 // could not check
	ExitUsage      = 3
	ExitAuth       = 4
	ExitNetwork    = 5
	ExitTool       = 6 // an external tool failed or is missing
)

// Stable machine codes used in the JSON error envelope.
const (
	CodeFailed     = "failed"
	CodeUnverified = "unverified"
	CodeUsage      = "usage"
	CodeAuth       = "auth"
	CodeNetwork    = "network"
	CodeTool       = "tool"
)

// Error is a command failure that knows its exit code.
type Error struct {
	Code    string // stable machine code, e.g. "usage", "auth", "network", "tool", "failed", "unverified"
	Exit    int
	Message string
	Hint    string
	Err     error
}

// Error renders the message and the wrapped cause, when there is one.
func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

// Unwrap exposes the underlying cause.
func (e *Error) Unwrap() error {
	return e.Err
}

// Failed reports that the operation ran and failed, or produced findings.
func Failed(msg string) *Error {
	return &Error{Code: CodeFailed, Exit: ExitFailed, Message: msg}
}

// Usage reports an invalid invocation.
func Usage(msg string) *Error {
	return &Error{Code: CodeUsage, Exit: ExitUsage, Message: msg}
}

// Auth reports missing or rejected credentials.
func Auth(msg string) *Error {
	return &Error{Code: CodeAuth, Exit: ExitAuth, Message: msg}
}

// Network reports a transport failure or an unreachable service.
func Network(msg string) *Error {
	return &Error{Code: CodeNetwork, Exit: ExitNetwork, Message: msg}
}

// Tool reports that an external tool failed or is missing.
func Tool(msg string) *Error {
	return &Error{Code: CodeTool, Exit: ExitTool, Message: msg}
}

// Unverified reports that a check could not be performed.
func Unverified(msg string) *Error {
	return &Error{Code: CodeUnverified, Exit: ExitUnverified, Message: msg}
}

// Wrap attaches err as the cause of c.
func Wrap(err error, c *Error) *Error {
	c.Err = err
	return c
}

// WithHint attaches a remediation hint to e.
func WithHint(e *Error, hint string) *Error {
	e.Hint = hint
	return e
}

// ExitCode maps err to the process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var c *Error
	if errors.As(err, &c) {
		return c.Exit
	}
	return ExitFailed
}

// Code maps err to its stable machine code.
func Code(err error) string {
	if err == nil {
		return ""
	}
	var c *Error
	if errors.As(err, &c) {
		return c.Code
	}
	return CodeFailed
}
