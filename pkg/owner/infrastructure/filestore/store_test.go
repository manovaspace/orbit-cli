package filestore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/owner/domain"
	"github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/filestore"
)

func TestStore_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "nested", "orbit", "owner.json")

	s := filestore.NewStore(storePath)

	if s.IsVerified() {
		t.Fatalf("expected unverified before save")
	}

	_, err := s.LoadOwner()
	if err != domain.ErrOwnerNotFound {
		t.Fatalf("expected ErrOwnerNotFound for missing store, got: %v", err)
	}

	secret, err := domain.GenerateMasterSecret()
	if err != nil {
		t.Fatalf("GenerateMasterSecret failed: %v", err)
	}

	rec := &domain.OwnerRecord{
		Email:             "admin@example.com",
		DisplayName:       "Alireza",
		VerifiedAt:        time.Now().UTC().Truncate(time.Second),
		RootSigningSecret: secret,
	}

	if err := s.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	// Verify file mode is 0600
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected file mode 0600, got %o", perm)
	}

	// Load record back
	loaded, err := s.LoadOwner()
	if err != nil {
		t.Fatalf("LoadOwner failed: %v", err)
	}

	if loaded.Email != rec.Email {
		t.Errorf("expected email %s, got %s", rec.Email, loaded.Email)
	}
	if loaded.DisplayName != rec.DisplayName {
		t.Errorf("expected display name %s, got %s", rec.DisplayName, loaded.DisplayName)
	}
	if loaded.RootSigningSecret != rec.RootSigningSecret {
		t.Errorf("expected secret %s, got %s", rec.RootSigningSecret, loaded.RootSigningSecret)
	}
	if loaded.KeyFingerprint == "" {
		t.Errorf("expected non-empty key fingerprint")
	}
	if !loaded.VerifiedAt.Equal(rec.VerifiedAt) {
		t.Errorf("expected verified_at %v, got %v", rec.VerifiedAt, loaded.VerifiedAt)
	}

	if !s.IsVerified() {
		t.Fatalf("expected IsVerified() to be true")
	}
}

func TestStore_DefaultPath(t *testing.T) {
	t.Run("ORBIT_OWNER_STORE env override", func(t *testing.T) {
		customPath := "/tmp/custom-owner.json"
		t.Setenv("ORBIT_OWNER_STORE", customPath)
		p := filestore.DefaultStorePath()
		if p != customPath {
			t.Fatalf("expected %s, got %s", customPath, p)
		}
	})

	t.Run("Default home config path", func(t *testing.T) {
		t.Setenv("ORBIT_OWNER_STORE", "")
		p := filestore.DefaultStorePath()
		if !strings.HasSuffix(p, filepath.Join(".config", "orbit", "owner.json")) && p != "/etc/orbit/owner.json" {
			t.Fatalf("unexpected default store path: %s", p)
		}
	})
}

func TestStore_InsecurePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "owner.json")
	s := filestore.NewStore(storePath)

	rec := &domain.OwnerRecord{
		Email:             "test@example.com",
		VerifiedAt:        time.Now().UTC(),
		RootSigningSecret: "secret123",
	}

	if err := s.SaveOwner(rec); err != nil {
		t.Fatalf("SaveOwner failed: %v", err)
	}

	if err := s.CheckPermissions(); err != nil {
		t.Fatalf("expected secure permissions check, got: %v", err)
	}

	// Make permissions insecure
	if err := os.Chmod(storePath, 0644); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	if err := s.CheckPermissions(); err != domain.ErrInsecurePermissions {
		t.Fatalf("expected ErrInsecurePermissions, got: %v", err)
	}
}
