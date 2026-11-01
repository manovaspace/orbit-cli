package application

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/uninstall/domain"
)

type fakeFS struct {
	entries   map[string]bool // path -> isDir
	globs     map[string][]string
	home      string
	exec      string
	removeErr map[string]error

	removed    []string
	removedAll []string
	refreshed  int
}

func newFakeFS() *fakeFS {
	return &fakeFS{entries: map[string]bool{}, globs: map[string][]string{}, removeErr: map[string]error{}}
}

func (f *fakeFS) Stat(path string) (domain.FileInfo, bool, error) {
	isDir, ok := f.entries[path]
	if !ok {
		return domain.FileInfo{}, false, nil
	}
	return domain.FileInfo{IsDir: isDir}, true, nil
}

func (f *fakeFS) RemoveElevated(_ context.Context, path string) error {
	if err := f.removeErr[path]; err != nil {
		return err
	}
	f.removed = append(f.removed, path)
	return nil
}

func (f *fakeFS) RemoveAll(path string) error {
	f.removedAll = append(f.removedAll, path)
	return nil
}

func (f *fakeFS) Glob(pattern string) ([]string, error) {
	return f.globs[pattern], nil
}

func (f *fakeFS) RefreshManPages(context.Context) { f.refreshed++ }

func (f *fakeFS) HomeDir() (string, error) { return f.home, nil }

func (f *fakeFS) Executable() (string, error) { return f.exec, nil }

func TestUninstallRemovesManPages(t *testing.T) {
	fs := newFakeFS()
	fs.home = "/home/test"
	fs.globs["/home/test/.local/share/man/man1/orbit*.1"] = []string{
		"/home/test/.local/share/man/man1/orbit.1",
		"/home/test/.local/share/man/man1/orbit-staff.1",
	}
	buf := &bytes.Buffer{}
	svc := &Service{FS: fs, Out: buf}

	res, err := svc.Uninstall(context.Background(), Input{Force: true})
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(res.RemovedManPages) != 2 {
		t.Errorf("removed man pages = %v", res.RemovedManPages)
	}
	if len(fs.removed) != 2 {
		t.Errorf("removed = %v", fs.removed)
	}
	if fs.refreshed != 1 {
		t.Errorf("expected one mandb refresh, got %d", fs.refreshed)
	}
	if !strings.Contains(buf.String(), "Removed man page") {
		t.Errorf("output = %s", buf.String())
	}
}

func TestUninstallRefusesPurgeWithDirtyWorkspace(t *testing.T) {
	fs := newFakeFS()
	fs.home = "/home/test"
	svc := &Service{FS: fs, Out: &bytes.Buffer{}}

	_, err := svc.Uninstall(context.Background(), Input{
		PurgeWorkspace: true,
		DirtyRepos:     []string{"repo (1 uncommitted changes)"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot purge workspace") {
		t.Fatalf("error = %v", err)
	}
}

func TestUninstallRemovesBinariesAndPurgesState(t *testing.T) {
	fs := newFakeFS()
	fs.home = "/home/test"
	fs.exec = "/home/test/.local/bin/orbit"
	fs.entries["/home/test/.local/bin/orbit"] = false
	fs.entries["/home/test/.orbit"] = true
	fs.entries["/home/test/.config/orbit"] = true
	buf := &bytes.Buffer{}
	svc := &Service{FS: fs, Out: buf}

	res, err := svc.Uninstall(context.Background(), Input{Force: true, PurgeState: true})
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(res.RemovedBinaries) != 1 || res.RemovedBinaries[0] != "/home/test/.local/bin/orbit" {
		t.Errorf("binary result = %+v", res)
	}
	if len(res.PurgedState) != 2 {
		t.Errorf("purged state = %+v", res.PurgedState)
	}
	if !strings.Contains(buf.String(), "Orbit CLI uninstalled successfully.") {
		t.Errorf("output = %s", buf.String())
	}
}

func TestUninstallNoBinaries(t *testing.T) {
	fs := newFakeFS()
	fs.home = "/home/test"
	buf := &bytes.Buffer{}
	svc := &Service{FS: fs, Out: buf}

	res, err := svc.Uninstall(context.Background(), Input{Force: true})
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !res.NoBinaries {
		t.Error("expected NoBinaries=true")
	}
	if !strings.Contains(buf.String(), "No active Orbit binaries found") {
		t.Errorf("output = %s", buf.String())
	}
}
