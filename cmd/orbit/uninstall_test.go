package main

import (
	"bytes"
	"context"
	"testing"

	uninstalldomain "github.com/manovaspace/orbit-cli/pkg/uninstall/domain"
)

type fakeUninstallFS struct {
	entries map[string]bool
	home    string

	removed    []string
	removedAll []string
}

func (f *fakeUninstallFS) Stat(path string) (uninstalldomain.FileInfo, bool, error) {
	isDir, ok := f.entries[path]
	if !ok {
		return uninstalldomain.FileInfo{}, false, nil
	}
	return uninstalldomain.FileInfo{IsDir: isDir}, true, nil
}

func (f *fakeUninstallFS) RemoveElevated(_ context.Context, path string) error {
	f.removed = append(f.removed, path)
	return nil
}

func (f *fakeUninstallFS) RemoveAll(path string) error {
	f.removedAll = append(f.removedAll, path)
	return nil
}

func (f *fakeUninstallFS) Glob(string) ([]string, error) { return nil, nil }

func (f *fakeUninstallFS) RefreshManPages(context.Context) {}

func (f *fakeUninstallFS) HomeDir() (string, error) { return f.home, nil }

func (f *fakeUninstallFS) Executable() (string, error) { return "", nil }

func TestUninstallJSONEnvelope(t *testing.T) {
	previous := uninstallFS
	t.Cleanup(func() { uninstallFS = previous })
	uninstallFS = &fakeUninstallFS{entries: map[string]bool{}, home: "/home/orbit-test"}

	t.Setenv("ORBIT_WORKSPACE", t.TempDir())
	out := &bytes.Buffer{}
	cmd := newUninstallCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--yes", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("uninstall --json: %v", err)
	}

	data := envelopeData(t, requireEnvelope(t, out))
	if data["noBinaries"] != true {
		t.Errorf("noBinaries = %v", data["noBinaries"])
	}
}
