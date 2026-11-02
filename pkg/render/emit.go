package render

import (
	"io"

	"github.com/manovaspace/orbit-cli/pkg/output"
)

// List is the standard page shape for JSON list results.
type List[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

// NewList builds a List. Nil items become an empty slice so JSON renders [].
// A limit of zero means "all items": page becomes 1 and limit becomes total.
func NewList[T any](items []T, total, page, limit int) List[T] {
	if items == nil {
		items = []T{}
	}
	if limit <= 0 {
		page = 1
		limit = total
	}
	return List[T]{Items: items, Total: total, Page: page, Limit: limit}
}

// Emit writes one command result either as a JSON envelope or as text. The
// text callback runs only in text mode; its error and the printer's first
// write error are both returned.
func Emit(out io.Writer, jsonMode bool, data any, text func(*Printer) error) error {
	if jsonMode {
		return output.Write(out, output.Success(data))
	}
	p := NewPrinter(out)
	if err := text(p); err != nil {
		return err
	}
	return p.Err()
}

// EmitFailure writes the failure envelope in JSON mode and returns err. In text
// mode it returns err untouched so the root command prints it.
func EmitFailure(out io.Writer, jsonMode bool, err error) error {
	if jsonMode {
		if writeErr := output.Write(out, output.Failure(err)); writeErr != nil {
			return writeErr
		}
	}
	return err
}

// EmitFindings writes a result that carries findings plus a failure. In JSON
// mode the envelope keeps both data and error; in text mode the callback prints
// the findings and failure is returned for exit-code mapping.
func EmitFindings(out io.Writer, jsonMode bool, data any, failure error, text func(*Printer) error) error {
	if jsonMode {
		envelope := output.Success(data)
		if failure != nil {
			envelope = output.Failure(failure)
			envelope.Data = data
		}
		if err := output.Write(out, envelope); err != nil {
			return err
		}
		return failure
	}
	p := NewPrinter(out)
	if err := text(p); err != nil {
		return err
	}
	if err := p.Err(); err != nil {
		return err
	}
	return failure
}
