package services

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLocalSecretsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "orbit", "local.env")
	want := map[string]string{"PG_APP_API_PASSWORD": "abc123", "PG_BILLING_PASSWORD": "zzz9"}
	if err := SaveLocalSecrets(path, want); err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)

	got, err := LocalSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LocalSecrets = %v, want %v", got, want)
	}

	missing, err := LocalSecrets(filepath.Join(t.TempDir(), "none.env"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing file = %v, %v; want empty, nil", missing, err)
	}

	if err := SaveLocalSecrets(path, map[string]string{"PG_X_PASSWORD": "a\nb"}); err == nil {
		t.Fatal("a value with a newline was saved")
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
