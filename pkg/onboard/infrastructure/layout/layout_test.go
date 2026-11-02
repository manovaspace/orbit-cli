package layout_test

import (
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/layout"
)

func TestLayoutDelegatesToTheCommandResolvers(t *testing.T) {
	l := layout.Layout{
		Root: func(override string) string { return "root:" + override },
		Manifest: func(root, flag string) string {
			return root + "/" + flag
		},
		Infra: func(root string) string { return root + "/orbit/orbit-infra" },
	}

	if got := l.WorkspaceRoot("/home/dev"); got != "root:/home/dev" {
		t.Errorf("WorkspaceRoot = %q", got)
	}
	if got := l.ManifestPath("/ws", "workspace.yaml"); got != "/ws/workspace.yaml" {
		t.Errorf("ManifestPath = %q", got)
	}
	if got := l.InfraDir("/ws"); got != "/ws/orbit/orbit-infra" {
		t.Errorf("InfraDir = %q", got)
	}
}

func TestLayoutWithoutResolvers(t *testing.T) {
	var l layout.Layout
	if got := l.WorkspaceRoot("/ws"); got != "/ws" {
		t.Errorf("WorkspaceRoot = %q, want the override back", got)
	}
	if got := l.ManifestPath("/ws", "custom.yaml"); got != "custom.yaml" {
		t.Errorf("ManifestPath = %q", got)
	}
	if got := l.InfraDir("/ws"); got != "" {
		t.Errorf("InfraDir = %q, want empty", got)
	}
}
