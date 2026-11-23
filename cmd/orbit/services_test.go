package main

import "testing"

// TestServicesCommandsAreNotServerOnly keeps up, down and services available on macOS, where the
// native services run. `dev up` and `dev down` stay server-only; they are a different path.
func TestServicesCommandsAreNotServerOnly(t *testing.T) {
	root := newRootCmd()
	for _, path := range [][]string{{"up"}, {"down"}, {"services"}} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("find %v: %v", path, err)
		}
		if got, ok := serverOnlyPath(cmd); ok {
			t.Errorf("%v is server-only under %q", path, got)
		}
	}
}
