package osfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestRefreshManPagesCommand(t *testing.T) {
	fake := &runnertest.Fake{}
	fake.On("mandb -q", runner.Result{}, nil)

	New(fake).RefreshManPages(context.Background())

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "mandb" || !reflect.DeepEqual(calls[0].Args, []string{"-q"}) {
		t.Errorf("call = %+v", calls[0])
	}
}

func TestStatGlobRemoveAll(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "orbit.1")
	if err := os.WriteFile(file, []byte("man"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := New(&runnertest.Fake{})
	if _, exists, err := fs.Stat(filepath.Join(dir, "absent")); err != nil || exists {
		t.Fatalf("missing stat: exists=%v err=%v", exists, err)
	}
	info, exists, err := fs.Stat(file)
	if err != nil || !exists || info.IsDir {
		t.Fatalf("file stat: info=%+v exists=%v err=%v", info, exists, err)
	}

	matches, err := fs.Glob(filepath.Join(dir, "orbit*.1"))
	if err != nil || len(matches) != 1 || matches[0] != file {
		t.Fatalf("glob = %v, err = %v", matches, err)
	}

	if err := fs.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected %s removed", dir)
	}
}
