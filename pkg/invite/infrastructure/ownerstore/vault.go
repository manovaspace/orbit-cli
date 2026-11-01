// Package ownerstore adapts the sealed owner vault to the invitation ports.
package ownerstore

import (
	"github.com/manovaspace/orbit-cli/pkg/owner/infrastructure/filestore"
)

// Vault reads the owner identity used to sign invitations.
type Vault struct {
	Store *filestore.Store
}

// New builds a Vault over store.
func New(store *filestore.Store) *Vault {
	return &Vault{Store: store}
}

// Owner returns the verified owner's email and signing secret.
func (v *Vault) Owner() (email string, secret string, verified bool) {
	if v == nil || v.Store == nil {
		return "", "", false
	}
	rec, err := v.Store.LoadOwner()
	if err != nil || rec == nil || !rec.IsVerified() {
		return "", "", false
	}
	return rec.Email, rec.RootSigningSecret, true
}
