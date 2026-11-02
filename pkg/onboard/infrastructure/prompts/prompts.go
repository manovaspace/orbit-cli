// Package prompts asks the developer questions on the command's stdin/stdout.
package prompts

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Prompter reads answers from In and writes questions to Out.
type Prompter struct {
	In  io.Reader
	Out io.Writer
}

// New builds a Prompter over r and w.
func New(r io.Reader, w io.Writer) *Prompter {
	return &Prompter{In: r, Out: w}
}

// YesNo asks a confirmation question. The suffix is already styled.
func (p *Prompter) YesNo(question, defaultSuffix string, defaultYes bool) bool {
	fmt.Fprintf(p.out(), "%s %s: ", question, defaultSuffix)
	scanner := bufio.NewScanner(p.in())
	if scanner.Scan() {
		text := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if text == "" {
			return defaultYes
		}
		return text == "y" || text == "yes"
	}
	return defaultYes
}

// String asks for a line of input, returning defaultValue on an empty answer.
func (p *Prompter) String(question, defaultSuffix, defaultValue string) string {
	if defaultSuffix != "" {
		fmt.Fprintf(p.out(), "%s %s: ", question, defaultSuffix)
	} else {
		fmt.Fprintf(p.out(), "%s: ", question)
	}
	scanner := bufio.NewScanner(p.in())
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return defaultValue
		}
		return text
	}
	return defaultValue
}

func (p *Prompter) in() io.Reader {
	if p.In == nil {
		return strings.NewReader("")
	}
	return p.In
}

func (p *Prompter) out() io.Writer {
	if p.Out == nil {
		return io.Discard
	}
	return p.Out
}
