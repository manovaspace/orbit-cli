package prompts_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/prompts"
)

func TestYesNoDefaultsAndAnswers(t *testing.T) {
	cases := []struct {
		in      string
		def     bool
		want    bool
		written string
	}{
		{in: "\n", def: true, want: true, written: "[Y/n]"},
		{in: "\n", def: false, want: false, written: "[y/N]"},
		{in: "y\n", def: false, want: true},
		{in: "YES\n", def: false, want: true},
		{in: "n\n", def: true, want: false},
		{in: "", def: true, want: true},
	}
	for _, tc := range cases {
		out := new(bytes.Buffer)
		got := prompts.New(strings.NewReader(tc.in), out).YesNo("Continue?", tc.written, tc.def)
		if got != tc.want {
			t.Errorf("answer for %q (default %v) = %v, want %v", tc.in, tc.def, got, tc.want)
		}
		if !strings.HasPrefix(out.String(), "Continue? ") {
			t.Errorf("prompt not written: %q", out.String())
		}
	}
}

func TestStringUsesDefaultOnEmptyAnswer(t *testing.T) {
	out := new(bytes.Buffer)
	p := prompts.New(strings.NewReader("\n"), out)
	if got := p.String("Token", "(abc)", "abc"); got != "abc" {
		t.Errorf("got %q, want the default", got)
	}
	if !strings.Contains(out.String(), "(abc)") {
		t.Errorf("default not rendered: %q", out.String())
	}

	out.Reset()
	p = prompts.New(strings.NewReader("typed\n"), out)
	if got := p.String("Token", "", ""); got != "typed" {
		t.Errorf("got %q, want typed", got)
	}
	if !strings.Contains(out.String(), "Token: ") {
		t.Errorf("prompt missing colon: %q", out.String())
	}
}

func TestPromptsTolerateMissingStreams(t *testing.T) {
	if got := prompts.New(nil, nil).YesNo("Q", "", true); got != true {
		t.Errorf("answer = %v, want the default", got)
	}
	if got := prompts.New(nil, nil).String("Q", "", "fallback"); got != "fallback" {
		t.Errorf("answer = %q, want the fallback", got)
	}
}
