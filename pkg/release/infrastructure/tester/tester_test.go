package tester

import (
	"errors"
	"testing"
)

type fakeFiles struct {
	files map[string]string
}

func (f *fakeFiles) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return []byte(data), nil
}

func (f *fakeFiles) Exists(path string) bool {
	_, ok := f.files[path]
	return ok
}

func (f *fakeFiles) ReadDir(string) ([]string, error) { return nil, nil }

func TestCommandDetectsGoAndBun(t *testing.T) {
	goTester := New(&fakeFiles{files: map[string]string{"/repo/go.mod": "module example.test\n"}})
	name, args, err := goTester.command("/repo")
	if err != nil {
		t.Fatalf("go command: %v", err)
	}
	if name != "go" || len(args) != 4 || args[0] != "test" {
		t.Errorf("go command = %s %v", name, args)
	}

	bunTester := New(&fakeFiles{files: map[string]string{
		"/repo/package.json": `{"scripts":{"test":"vitest run"}}`,
	}})
	name, args, err = bunTester.command("/repo")
	if err != nil {
		t.Fatalf("bun command: %v", err)
	}
	if name != "bun" || len(args) != 2 || args[0] != "run" || args[1] != "test" {
		t.Errorf("bun command = %s %v", name, args)
	}

	plainTester := New(&fakeFiles{files: map[string]string{"/repo/package.json": `{}`}})
	name, args, err = plainTester.command("/repo")
	if err != nil {
		t.Fatalf("bun test command: %v", err)
	}
	if name != "bun" || len(args) != 1 || args[0] != "test" {
		t.Errorf("plain command = %s %v", name, args)
	}
}

func TestCommandMissingIsUnverified(t *testing.T) {
	_, _, err := New(&fakeFiles{files: map[string]string{}}).command("/repo")
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestMergeEnvOverridesAndSecretValues(t *testing.T) {
	merged := mergeEnv(map[string]string{"CI_TIER": "release", "TEST_DATABASE_URL": "postgres://u:p@h/db"})
	found := map[string]bool{}
	for _, entry := range merged {
		if entry == "CI_TIER=release" {
			found["tier"] = true
		}
		if entry == "TEST_DATABASE_URL=postgres://u:p@h/db" {
			found["db"] = true
		}
	}
	if !found["tier"] || !found["db"] {
		t.Errorf("merged env missing overrides: %v", merged)
	}

	secrets := secretValues(map[string]string{"TEST_DATABASE_URL": "postgres://u:p@h/db", "CI_TIER": "release"})
	if len(secrets) != 1 || secrets[0] != "postgres://u:p@h/db" {
		t.Errorf("secrets = %v", secrets)
	}
}
