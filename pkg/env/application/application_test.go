package application

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/env/domain"
)

type fakeFile struct {
	data []byte
	perm uint32
}

// fakeFS is an in-memory filesystem with unix permission bits.
type fakeFS struct {
	files map[string]fakeFile
	dirs  map[string]bool
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: map[string]fakeFile{}, dirs: map[string]bool{}}
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	file, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return file.data, nil
}

func (f *fakeFS) WriteFile(path string, data []byte, perm uint32) error {
	f.files[path] = fakeFile{data: append([]byte(nil), data...), perm: perm}
	return nil
}

func (f *fakeFS) Chmod(path string, perm uint32) error {
	file, ok := f.files[path]
	if !ok {
		return fs.ErrNotExist
	}
	file.perm = perm
	f.files[path] = file
	return nil
}

func (f *fakeFS) MkdirAll(path string) error {
	f.dirs[path] = true
	return nil
}

func (f *fakeFS) Stat(path string) (FileInfo, bool, error) {
	if file, ok := f.files[path]; ok {
		return FileInfo{Perm: file.perm}, true, nil
	}
	if f.dirs[path] {
		return FileInfo{IsDir: true, Perm: 0o755}, true, nil
	}
	return FileInfo{}, false, nil
}

type fakeFinder struct {
	schemas []string
	err     error
}

func (f *fakeFinder) Find(string) ([]string, error) { return f.schemas, f.err }

type fakeLoader struct {
	schemas map[string]*domain.EnvSchema
	err     error
}

func (f *fakeLoader) Load(path string) (*domain.EnvSchema, error) {
	if f.err != nil {
		return nil, f.err
	}
	schema, ok := f.schemas[path]
	if !ok {
		return nil, errors.New("schema missing")
	}
	return schema, nil
}

type fakeMCP struct{ err error }

func (f *fakeMCP) Ensure(string) error { return f.err }

func testSchema() *domain.EnvSchema {
	return &domain.EnvSchema{Version: "1", Variables: []domain.VariableDef{
		{Name: "PORT", Type: domain.TypeInteger, Default: "10050", Required: true, Description: "Port"},
		{Name: "DATABASE_URL", Type: domain.TypeURL, Required: true},
	}}
}

func newCheckService(files *fakeFS, finder *fakeFinder, loader *fakeLoader, out *bytes.Buffer) *Service {
	return &Service{FS: files, Schemas: finder, Loader: loader, MCP: &fakeMCP{}, Out: out}
}

func TestCheckValidContract(t *testing.T) {
	root := "/workspace"
	schemaPath := filepath.Join(root, "svc", ".env.schema.yaml")
	files := newFakeFS()
	files.files[filepath.Join(root, "svc", ".env")] = fakeFile{data: []byte("PORT=10050\nDATABASE_URL=https://example.test/db\n"), perm: 0o600}
	out := new(bytes.Buffer)
	svc := newCheckService(files, &fakeFinder{schemas: []string{schemaPath}}, &fakeLoader{schemas: map[string]*domain.EnvSchema{schemaPath: testSchema()}}, out)

	res, err := svc.Check(context.Background(), CheckInput{SearchPath: root, WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !res.Verified || res.ValidCount != 1 || res.ErrorCount != 0 {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(out.String(), ".env valid") {
		t.Errorf("missing valid line: %s", out.String())
	}
}

func TestCheckInvalidValuesRedacted(t *testing.T) {
	root := "/workspace"
	schemaPath := filepath.Join(root, ".env.schema.yaml")
	files := newFakeFS()
	files.files[filepath.Join(root, ".env")] = fakeFile{data: []byte("DATABASE_URL=https://user:synthetic-cli-secret@bad host\n")}
	out := new(bytes.Buffer)
	svc := newCheckService(files, &fakeFinder{schemas: []string{schemaPath}}, &fakeLoader{schemas: map[string]*domain.EnvSchema{schemaPath: testSchema()}}, out)

	_, err := svc.Check(context.Background(), CheckInput{SearchPath: root, WorkspaceRoot: root})
	if err == nil {
		t.Fatal("invalid URL passed")
	}
	if clierr.ExitCode(err) != clierr.ExitFailed {
		t.Errorf("exit = %d, want failed", clierr.ExitCode(err))
	}
	if strings.Contains(out.String(), "synthetic-cli-secret") {
		t.Fatalf("secret leaked: %s", out.String())
	}
	if !strings.Contains(out.String(), "DATABASE_URL") {
		t.Fatalf("variable unidentified: %s", out.String())
	}
}

func TestCheckNoContractChecksPermissions(t *testing.T) {
	root := "/workspace"
	files := newFakeFS()
	files.files[filepath.Join(root, ".cursor", "mcp.env")] = fakeFile{data: []byte("SECRET=canary\n"), perm: 0o644}
	out := new(bytes.Buffer)
	svc := newCheckService(files, &fakeFinder{}, &fakeLoader{}, out)

	if _, err := svc.Check(context.Background(), CheckInput{SearchPath: root, WorkspaceRoot: root}); err == nil {
		t.Error("absent contracts incorrectly successful")
	}
	if !strings.Contains(out.String(), "permissions") {
		t.Errorf("permission check skipped: %s", out.String())
	}
	if strings.Contains(out.String(), "✔ 0 schemas valid") {
		t.Error("absent contracts rendered as green validation")
	}
	if !strings.Contains(out.String(), "unverified") {
		t.Errorf("missing unverified status: %s", out.String())
	}
}

func TestCheckDiscoveryFailureIsUnverified(t *testing.T) {
	svc := newCheckService(newFakeFS(), &fakeFinder{err: errors.New("missing")}, &fakeLoader{}, new(bytes.Buffer))
	_, err := svc.Check(context.Background(), CheckInput{SearchPath: "/missing", WorkspaceRoot: "/missing"})
	if err == nil {
		t.Fatal("missing search root treated as success")
	}
	if clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Errorf("exit = %d, want unverified", clierr.ExitCode(err))
	}
}

func TestSetupGeneratesAndSkips(t *testing.T) {
	root := "/workspace"
	first := filepath.Join(root, "a", ".env.schema.yaml")
	second := filepath.Join(root, "b", ".env.schema.yaml")
	files := newFakeFS()
	files.files[filepath.Join(root, "b", ".env")] = fakeFile{data: []byte("OLD=1"), perm: 0o644}
	out := new(bytes.Buffer)
	svc := &Service{
		FS:      files,
		Schemas: &fakeFinder{schemas: []string{first, second}},
		Loader:  &fakeLoader{schemas: map[string]*domain.EnvSchema{first: testSchema(), second: testSchema()}},
		MCP:     &fakeMCP{},
		Out:     out,
	}

	res, err := svc.Setup(context.Background(), SetupInput{SearchPath: root, WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Generated != 1 || res.Skipped != 1 {
		t.Fatalf("result = %+v", res)
	}
	generated, ok := files.files[filepath.Join(root, "a", ".env")]
	if !ok {
		t.Fatal("missing generated .env")
	}
	if generated.perm != 0o600 {
		t.Errorf("permissions = %04o, want 0600", generated.perm)
	}
	if !strings.Contains(string(generated.data), "PORT=10050") {
		t.Errorf("generated content = %s", generated.data)
	}
	if !strings.Contains(out.String(), ".env already exists") {
		t.Errorf("skip not reported: %s", out.String())
	}
}

func TestSetupForceOverwrites(t *testing.T) {
	root := "/workspace"
	schemaPath := filepath.Join(root, ".env.schema.yaml")
	files := newFakeFS()
	files.files[filepath.Join(root, ".env")] = fakeFile{data: []byte("OLD=1"), perm: 0o644}
	svc := &Service{
		FS:      files,
		Schemas: &fakeFinder{schemas: []string{schemaPath}},
		Loader:  &fakeLoader{schemas: map[string]*domain.EnvSchema{schemaPath: testSchema()}},
		MCP:     &fakeMCP{},
		Out:     new(bytes.Buffer),
	}
	res, err := svc.Setup(context.Background(), SetupInput{SearchPath: root, WorkspaceRoot: root, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generated != 1 || res.Skipped != 0 {
		t.Fatalf("result = %+v", res)
	}
	if strings.Contains(string(files.files[filepath.Join(root, ".env")].data), "OLD=1") {
		t.Error("forced setup kept old content")
	}
}

func TestSetupNoContractIsUnverified(t *testing.T) {
	svc := &Service{FS: newFakeFS(), Schemas: &fakeFinder{}, Loader: &fakeLoader{}, MCP: &fakeMCP{}, Out: new(bytes.Buffer)}
	_, err := svc.Setup(context.Background(), SetupInput{SearchPath: "/none", WorkspaceRoot: "/none"})
	if clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Fatalf("exit = %d, want unverified", clierr.ExitCode(err))
	}
}
