package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

func TestExitCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain", errors.New("boom"), 1},
		{"failed", clierr.Failed("findings"), 1},
		{"unverified", clierr.Unverified("could not check"), 2},
		{"usage", clierr.Usage("bad flag"), 3},
		{"auth", clierr.Auth("denied"), 4},
		{"network", clierr.Network("unreachable"), 5},
		{"tool", clierr.Tool("git missing"), 6},
		{"wrapped", fmt.Errorf("pr create: %w", clierr.Auth("denied")), 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor = %d, want %d", got, tc.want)
			}
		})
	}
}
