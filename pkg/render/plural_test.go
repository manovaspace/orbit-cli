package render_test

import (
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/render"
)

func TestPlural(t *testing.T) {
	cases := []struct {
		n        int
		singular string
		plural   string
		want     string
	}{
		{n: 0, singular: "repository", plural: "repositories", want: "repositories"},
		{n: 1, singular: "repository", plural: "repositories", want: "repository"},
		{n: 2, singular: "repository", plural: "repositories", want: "repositories"},
		{n: 1, singular: "warning", plural: "warnings", want: "warning"},
	}
	for _, tc := range cases {
		if got := render.Plural(tc.n, tc.singular, tc.plural); got != tc.want {
			t.Errorf("Plural(%d, %q, %q) = %q, want %q", tc.n, tc.singular, tc.plural, got, tc.want)
		}
	}
}
