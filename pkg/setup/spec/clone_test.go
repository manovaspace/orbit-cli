package spec

import "testing"

func TestCloneURL(t *testing.T) {
	cases := []struct {
		in, protocol, want string
	}{
		{"git@github.com:o/r.git", "https", "https://github.com/o/r.git"},
		{"https://github.com/o/r.git", "ssh", "git@github.com:o/r.git"},
		{"https://github.com/o/r.git", "https", "https://github.com/o/r.git"},
		{"git@github.com:o/r.git", "ssh", "git@github.com:o/r.git"},
		{"https://gitlab.com/x/y.git", "ssh", "https://gitlab.com/x/y.git"},
		{"git@gitlab.com:x/y.git", "https", "git@gitlab.com:x/y.git"},
	}
	for _, c := range cases {
		got, err := CloneURL(c.in, c.protocol)
		if err != nil {
			t.Fatalf("CloneURL(%q,%q): %v", c.in, c.protocol, err)
		}
		if got != c.want {
			t.Fatalf("CloneURL(%q,%q): got %q want %q", c.in, c.protocol, got, c.want)
		}
	}
	if _, err := CloneURL("https://github.com/o/r.git", "http"); err == nil {
		t.Fatal("unknown protocol: want error")
	}
}

func TestHandbookConstants(t *testing.T) {
	if HandbookRepo != "manovaspace/handbook" || HandbookPath != "handbook" {
		t.Fatalf("handbook constants: %q %q", HandbookRepo, HandbookPath)
	}
}
