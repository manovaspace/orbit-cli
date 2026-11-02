// Package render is the shared output layer for Orbit commands. It implements
// ADR-047 phase 1 and encodes the contract later phases migrate commands onto.
//
// One result path (D1): a command computes its result once, then renders either
// the JSON envelope or the text form from that same value. It never builds the
// text form separately from the machine-readable one.
//
// Text shape (D2): a result starts with a title, may continue with sections,
// then a table or a key/value body, and ends with at most one summary line.
// A failure follows with a hint line. Empty results use the single shared
// "No <what> found." wording.
//
// JSON envelope (D4): output.Write wraps the result in the versioned envelope
// from package output. The data member is always an object, field names are
// camelCase, and list results use {items,total,page,limit} through List.
//
// Standard output only (D5): results go to stdout and diagnostics or progress
// go to stderr. A JSON run prints exactly one line.
//
// The package follows ADR-047; existing commands migrate onto it in phase 3.
package render
