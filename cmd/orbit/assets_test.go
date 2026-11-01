package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assetsapp "github.com/manovaspace/orbit-cli/pkg/assets/application"
	assetsmem "github.com/manovaspace/orbit-cli/pkg/assets/infrastructure/memstore"
	assetsosfs "github.com/manovaspace/orbit-cli/pkg/assets/infrastructure/osfs"
)

const testWorkspaceYAML = `version: "1"
workspace: "test"
groups:
  test:
    path: ""
    repositories:
      - name: "repo"
        path: "repo"
        required: true
`

func setupAssetsTest(t *testing.T) (string, *assetsmem.Store) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)
	if err := os.WriteFile(filepath.Join(root, "workspace.yaml"), []byte(testWorkspaceYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "repo", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := assetsmem.New()
	prevStore, prevFS := openAssetsStore, assetsFS
	openAssetsStore = func(context.Context) (assetsapp.Store, error) { return store, nil }
	assetsFS = assetsosfs.New()
	t.Cleanup(func() {
		openAssetsStore, assetsFS = prevStore, prevFS
	})
	return filepath.Join(root, "repo"), store
}

func writeAssetManifest(t *testing.T, repoRoot, rel string, payload []byte) {
	t.Helper()
	sum := sha256.Sum256(payload)
	body := fmt.Sprintf("version: 1\nobjects:\n  - path: %s\n    sha256: %s\n    size: %d\n", rel, hex.EncodeToString(sum[:]), len(payload))
	if err := os.WriteFile(filepath.Join(repoRoot, "orbit-assets.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAssetsStatusJSON(t *testing.T) {
	repo, _ := setupAssetsTest(t)
	payload := []byte("pdf")
	writeAssetManifest(t, repo, "docs/a.pdf", payload)
	if err := os.WriteFile(filepath.Join(repo, "docs", "a.pdf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runRootForTest(t, "assets", "status", "--json")
	if err != nil {
		t.Fatalf("assets status --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Problems     int `json:"problems"`
		Repositories []struct {
			Path  string `json:"path"`
			Files []struct {
				Path  string `json:"path"`
				State string `json:"state"`
			} `json:"files"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Problems != 0 {
		t.Errorf("problems = %d, want 0", data.Problems)
	}
	if len(data.Repositories) != 1 || data.Repositories[0].Path != "repo" {
		t.Fatalf("repositories = %+v", data.Repositories)
	}
	if len(data.Repositories[0].Files) != 1 || data.Repositories[0].Files[0].State != "ok" {
		t.Fatalf("files = %+v", data.Repositories[0].Files)
	}
}

func TestAssetsStatusJSONReportsMissing(t *testing.T) {
	repo, _ := setupAssetsTest(t)
	writeAssetManifest(t, repo, "docs/a.pdf", []byte("pdf"))

	out, err := runRootForTest(t, "assets", "status", "--json")
	if err == nil {
		t.Fatal("expected missing assets to fail")
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if env.Error == nil || env.Error.Code != "failed" {
		t.Fatalf("unexpected error body: %+v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "missing or mismatched") {
		t.Errorf("message = %q", env.Error.Message)
	}
}

func TestAssetsPullJSONRestoresFile(t *testing.T) {
	repo, store := setupAssetsTest(t)
	payload := []byte("pdf")
	writeAssetManifest(t, repo, "docs/a.pdf", payload)
	sum := sha256.Sum256(payload)
	if err := store.Put(context.Background(), hex.EncodeToString(sum[:]), strings.NewReader(string(payload)), int64(len(payload)), "application/pdf", "docs/a.pdf"); err != nil {
		t.Fatal(err)
	}

	out, err := runRootForTest(t, "assets", "pull", "--json")
	if err != nil {
		t.Fatalf("assets pull --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Repositories []string `json:"repositories"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if len(data.Repositories) != 1 || data.Repositories[0] != "repo" {
		t.Fatalf("repositories = %v", data.Repositories)
	}
	restored, err := os.ReadFile(filepath.Join(repo, "docs", "a.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "pdf" {
		t.Fatalf("restored = %q", restored)
	}
}

func TestAssetsPushJSONUploadsMissing(t *testing.T) {
	repo, store := setupAssetsTest(t)
	payload := []byte("pdf")
	writeAssetManifest(t, repo, "docs/a.pdf", payload)
	if err := os.WriteFile(filepath.Join(repo, "docs", "a.pdf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runRootForTest(t, "assets", "push", "--json")
	if err != nil {
		t.Fatalf("assets push --json: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if store.Len() != 1 {
		t.Fatalf("stored objects = %d, want 1", store.Len())
	}
}
