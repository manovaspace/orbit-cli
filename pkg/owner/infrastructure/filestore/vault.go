package filestore

import "github.com/manovaspace/orbit-cli/pkg/owner/domain"

// Vault adapts the owner file store to the owner application vault port.
type Vault struct {
	Store *Store
}

// NewVault builds a Vault at customPath (empty means the default path).
func NewVault(customPath string) *Vault {
	return &Vault{Store: NewStore(customPath)}
}

// Load reads the sealed owner record.
func (v *Vault) Load() (*domain.OwnerRecord, error) {
	return v.Store.LoadOwner()
}

// Save seals an owner record to disk.
func (v *Vault) Save(rec *domain.OwnerRecord) error {
	return v.Store.SaveOwner(rec)
}

// CheckPermissions verifies the vault file mode.
func (v *Vault) CheckPermissions() error {
	return v.Store.CheckPermissions()
}

// Exists reports whether the vault file is present.
func (v *Vault) Exists() bool {
	return v.Store.Exists()
}

// FilePath returns the vault path.
func (v *Vault) FilePath() string {
	return v.Store.FilePath()
}
