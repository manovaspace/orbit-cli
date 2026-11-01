package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/assets/domain"
)

// fakeFS is an in-memory repository filesystem.
type fakeFS struct {
	files map[string][]byte
	dirs  map[string]bool
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: map[string][]byte{}, dirs: map[string]bool{}}
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *fakeFS) WriteFile(path string, data []byte, _ uint32) error {
	f.files[path] = append([]byte(nil), data...)
	return nil
}

func (f *fakeFS) MkdirAll(path string) error {
	f.dirs[path] = true
	return nil
}

func (f *fakeFS) Rename(oldPath, newPath string) error {
	data, ok := f.files[oldPath]
	if !ok {
		return os.ErrNotExist
	}
	f.files[newPath] = data
	delete(f.files, oldPath)
	return nil
}

func (f *fakeFS) Remove(path string) error {
	delete(f.files, path)
	return nil
}

func (f *fakeFS) Stat(path string) (FileInfo, bool, error) {
	if f.dirs[path] {
		return FileInfo{IsDir: true}, true, nil
	}
	if _, ok := f.files[path]; ok {
		return FileInfo{}, true, nil
	}
	return FileInfo{}, false, nil
}

func (f *fakeFS) Open(path string) (io.ReadCloser, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeFS) HashFile(path string) (string, int64, error) {
	data, ok := f.files[path]
	if !ok {
		return "", 0, os.ErrNotExist
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), int64(len(data)), nil
}

// fakeStore is an in-memory object store.
type fakeStore struct {
	objects map[string][]byte
	puts    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{objects: map[string][]byte{}}
}

func (s *fakeStore) Put(_ context.Context, sha256 string, r io.Reader, _ int64, _, _ string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.objects[sha256] = data
	s.puts++
	return nil
}

func (s *fakeStore) Get(_ context.Context, sha256 string) (io.ReadCloser, error) {
	data, ok := s.objects[sha256]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeStore) Exists(_ context.Context, sha256 string) (bool, error) {
	_, ok := s.objects[sha256]
	return ok, nil
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newService(fs *fakeFS, store *fakeStore) *Service {
	return &Service{FS: fs, R2: store, Out: io.Discard}
}

func TestAddUploadsAndWritesManifest(t *testing.T) {
	ctx := context.Background()
	fs := newFakeFS()
	payload := []byte("pdf-bytes")
	fs.files[filepath.Join("/repo", "docs", "a.pdf")] = payload
	store := newFakeStore()

	obj, err := newService(fs, store).Add(ctx, "/repo", "docs/a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if obj.Object.Size != int64(len(payload)) {
		t.Fatalf("size=%d", obj.Object.Size)
	}
	if _, ok := store.objects[obj.Object.SHA256]; !ok {
		t.Fatal("object not uploaded")
	}
	m, err := newService(fs, store).LoadManifest("/repo")
	if err != nil {
		t.Fatal(err)
	}
	found, ok := m.Find("docs/a.pdf")
	if !ok || found.SHA256 != obj.Object.SHA256 {
		t.Fatalf("manifest: %+v", m)
	}
	gitignore := string(fs.files[filepath.Join("/repo", ".gitignore")])
	if !strings.Contains(gitignore, "docs/a.pdf") {
		t.Fatalf("gitignore: %s", gitignore)
	}
}

func TestAddSkipsUploadWhenHashExists(t *testing.T) {
	ctx := context.Background()
	fs := newFakeFS()
	fs.files[filepath.Join("/repo", "a.bin")] = []byte("same")
	store := newFakeStore()
	svc := newService(fs, store)

	first, err := svc.Add(ctx, "/repo", "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	store.puts = 0
	second, err := svc.Add(ctx, "/repo", "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	if first.Object.SHA256 != second.Object.SHA256 {
		t.Fatalf("%s vs %s", first.Object.SHA256, second.Object.SHA256)
	}
	if store.puts != 0 {
		t.Fatalf("unexpected put count %d", store.puts)
	}
}

func TestAddWarnsForSmallFiles(t *testing.T) {
	fs := newFakeFS()
	fs.files[filepath.Join("/repo", "icon.png")] = []byte("png")
	res, err := newService(fs, newFakeStore()).Add(context.Background(), "/repo", "icon.png")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Small {
		t.Error("small file not flagged")
	}
}

func TestPullRestoresMissingFile(t *testing.T) {
	ctx := context.Background()
	fs := newFakeFS()
	store := newFakeStore()
	payload := []byte("png")
	store.objects[hashOf(payload)] = payload
	if err := (newService(fs, store)).SaveManifest("/ws/repo", domain.Manifest{Version: 1, Objects: []domain.Object{{
		Path: "n.png", SHA256: hashOf(payload), Size: int64(len(payload)),
	}}}); err != nil {
		t.Fatal(err)
	}

	res, err := newService(fs, store).Pull(ctx, "/ws", []string{"repo"}, PullOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Repositories) != 1 || res.Repositories[0] != "repo" {
		t.Fatalf("repositories = %v", res.Repositories)
	}
	got := fs.files[filepath.Join("/ws", "repo", "n.png")]
	if string(got) != "png" {
		t.Fatalf("got %q", got)
	}
}

func TestPullMismatchWithoutForceErrors(t *testing.T) {
	fs := newFakeFS()
	store := newFakeStore()
	payload := []byte("png")
	store.objects[hashOf(payload)] = payload
	fs.files[filepath.Join("/ws", "repo", "n.png")] = []byte("changed")
	if err := (newService(fs, store)).SaveManifest("/ws/repo", domain.Manifest{Version: 1, Objects: []domain.Object{{
		Path: "n.png", SHA256: hashOf(payload), Size: int64(len(payload)),
	}}}); err != nil {
		t.Fatal(err)
	}

	err := (newService(fs, store)).pullRepo(context.Background(), "/ws/repo", PullOptions{})
	if !errors.Is(err, domain.ErrHashMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestStatusReportsMissing(t *testing.T) {
	fs := newFakeFS()
	if err := (newService(fs, newFakeStore())).SaveManifest("/ws/repo", domain.Manifest{Version: 1, Objects: []domain.Object{{
		Path: "gone.pdf", SHA256: "abc", Size: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	res, err := newService(fs, newFakeStore()).Status(context.Background(), "/ws", []string{"repo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Problems != 1 {
		t.Fatalf("problems=%d", res.Problems)
	}
	if len(res.Repositories) != 1 || len(res.Repositories[0].Files) != 1 {
		t.Fatalf("%+v", res)
	}
	if res.Repositories[0].Files[0].State != domain.FileMissing {
		t.Fatalf("%+v", res.Repositories[0].Files[0])
	}
}

func TestManifestMissingReturnsEmpty(t *testing.T) {
	m, err := newService(newFakeFS(), newFakeStore()).LoadManifest("/repo")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Version != 1 || len(m.Objects) != 0 {
		t.Fatalf("manifest: %+v", m)
	}
}

func TestEnsureGitignoreAddsPathOnce(t *testing.T) {
	fs := newFakeFS()
	svc := newService(fs, newFakeStore())
	if err := svc.ensureGitignore("/repo", "docs/a.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ensureGitignore("/repo", "docs/a.pdf"); err != nil {
		t.Fatal(err)
	}
	content := string(fs.files[filepath.Join("/repo", ".gitignore")])
	if strings.Count(content, "docs/a.pdf") != 1 {
		t.Fatalf("duplicated path: %s", content)
	}
}

func TestPushUploadsOnlyMissingObjects(t *testing.T) {
	ctx := context.Background()
	fs := newFakeFS()
	store := newFakeStore()
	payload := []byte("payload")
	fs.files[filepath.Join("/ws", "repo", "a.bin")] = payload
	if err := (newService(fs, store)).SaveManifest("/ws/repo", domain.Manifest{Version: 1, Objects: []domain.Object{{
		Path: "a.bin", SHA256: hashOf(payload), Size: int64(len(payload)),
	}}}); err != nil {
		t.Fatal(err)
	}

	res, err := newService(fs, store).Push(ctx, "/ws", []string{"repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Repositories) != 1 {
		t.Fatalf("repositories = %v", res.Repositories)
	}
	if _, ok := store.objects[hashOf(payload)]; !ok {
		t.Fatal("object not uploaded")
	}

	store.puts = 0
	if _, err := newService(fs, store).Push(ctx, "/ws", []string{"repo"}); err != nil {
		t.Fatal(err)
	}
	if store.puts != 0 {
		t.Fatalf("existing object uploaded again: %d puts", store.puts)
	}
}
