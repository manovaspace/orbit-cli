package setup

import (
	"reflect"
	"testing"
)

// TestDefaultPartOrder pins the run order of o setup: every part's needs appear before it.
func TestDefaultPartOrder(t *testing.T) {
	var names []string
	for _, p := range Default() {
		names = append(names, p.Name())
	}
	want := []string{"system", "identity", "handbook", "repos", "root", "toolchains", "services", "env", "prepare"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Default() order = %v, want %v", names, want)
	}
}
