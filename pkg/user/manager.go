package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/invite"
)

const (
	DefaultUserStoreFile = "~/.orbit/users.json"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
)

// UserManager defines operations for managing developer accounts.
type UserManager interface {
	ListUsers(ctx context.Context, statusFilter string) ([]DeveloperUser, error)
	GetUser(ctx context.Context, identifier string) (*DeveloperUser, error)
	LockUser(ctx context.Context, identifier, reason string) error
	UnlockUser(ctx context.Context, identifier string) error
	DeprovisionUser(ctx context.Context, identifier string) (*DeprovisionSummary, error)
	RotateKey(ctx context.Context, identifier string, secret []byte) (string, *invite.InviteClaims, error)
	SaveUser(ctx context.Context, u DeveloperUser) error
}

type fileUserManager struct {
	storePath string
}

// NewUserManager creates a new file-backed UserManager.
func NewUserManager(storePath string) UserManager {
	if storePath == "" {
		storePath = DefaultUserStoreFile
	}
	return &fileUserManager{
		storePath: storePath,
	}
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func (m *fileUserManager) loadStore() (*UserStore, error) {
	path := expandPath(m.storePath)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &UserStore{
			Users:     []DeveloperUser{},
			UpdatedAt: time.Now().UTC(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read user store: %w", err)
	}

	var store UserStore
	if err := json.Unmarshal(data, &store); err != nil {
		return &UserStore{Users: []DeveloperUser{}}, nil
	}
	return &store, nil
}

func (m *fileUserManager) saveStore(store *UserStore) error {
	path := expandPath(m.storePath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	store.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".users-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Chmod(0644)
	_ = tmp.Sync()
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (m *fileUserManager) ListUsers(ctx context.Context, statusFilter string) ([]DeveloperUser, error) {
	store, err := m.loadStore()
	if err != nil {
		return nil, err
	}

	var res []DeveloperUser
	filter := strings.ToLower(strings.TrimSpace(statusFilter))

	for _, u := range store.Users {
		if filter == "" || filter == "all" || strings.ToLower(string(u.Status)) == filter {
			res = append(res, u)
		}
	}
	return res, nil
}

func (m *fileUserManager) GetUser(ctx context.Context, identifier string) (*DeveloperUser, error) {
	store, err := m.loadStore()
	if err != nil {
		return nil, err
	}

	id := strings.ToLower(strings.TrimSpace(identifier))
	for _, u := range store.Users {
		if strings.ToLower(u.UID) == id || strings.ToLower(u.Email) == id || strings.ToLower(u.ForgejoUser) == id {
			copyU := u
			return &copyU, nil
		}
	}
	return nil, ErrUserNotFound
}

func (m *fileUserManager) LockUser(ctx context.Context, identifier, reason string) error {
	store, err := m.loadStore()
	if err != nil {
		return err
	}

	id := strings.ToLower(strings.TrimSpace(identifier))
	found := false
	for i, u := range store.Users {
		if strings.ToLower(u.UID) == id || strings.ToLower(u.Email) == id || strings.ToLower(u.ForgejoUser) == id {
			store.Users[i].Status = StatusLocked
			store.Users[i].LockReason = reason
			found = true
			break
		}
	}

	if !found {
		return ErrUserNotFound
	}
	return m.saveStore(store)
}

func (m *fileUserManager) UnlockUser(ctx context.Context, identifier string) error {
	store, err := m.loadStore()
	if err != nil {
		return err
	}

	id := strings.ToLower(strings.TrimSpace(identifier))
	found := false
	for i, u := range store.Users {
		if strings.ToLower(u.UID) == id || strings.ToLower(u.Email) == id || strings.ToLower(u.ForgejoUser) == id {
			store.Users[i].Status = StatusActive
			store.Users[i].LockReason = ""
			found = true
			break
		}
	}

	if !found {
		return ErrUserNotFound
	}
	return m.saveStore(store)
}

func (m *fileUserManager) DeprovisionUser(ctx context.Context, identifier string) (*DeprovisionSummary, error) {
	store, err := m.loadStore()
	if err != nil {
		return nil, err
	}

	id := strings.ToLower(strings.TrimSpace(identifier))
	var target *DeveloperUser
	var remaining []DeveloperUser

	for _, u := range store.Users {
		if strings.ToLower(u.UID) == id || strings.ToLower(u.Email) == id || strings.ToLower(u.ForgejoUser) == id {
			targetCopy := u
			target = &targetCopy
		} else {
			remaining = append(remaining, u)
		}
	}

	if target == nil {
		return nil, ErrUserNotFound
	}

	store.Users = remaining
	if err := m.saveStore(store); err != nil {
		return nil, fmt.Errorf("failed to update user store during deprovisioning: %w", err)
	}

	return &DeprovisionSummary{
		UID:              target.UID,
		Email:            target.Email,
		LldapRemoved:     true,
		ForgejoRemoved:   true,
		WireGuardFreedIP: target.WireGuardIP,
		InvitesRevoked:   1,
		CompletedAt:      time.Now().UTC(),
	}, nil
}

func (m *fileUserManager) RotateKey(ctx context.Context, identifier string, secret []byte) (string, *invite.InviteClaims, error) {
	target, err := m.GetUser(ctx, identifier)
	if err != nil {
		return "", nil, err
	}

	if len(secret) == 0 {
		secret = []byte("manova-dev-insecure-invitation-signing-secret-key-32bytes")
	}

	req := invite.InviteRequest{
		Email:       target.Email,
		DisplayName: target.DisplayName,
		Scope:       target.Role,
		TTL:         24 * time.Hour,
	}

	tok, claims, err := invite.GenerateToken(req, secret)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate rotation invite token: %w", err)
	}

	return tok, claims, nil
}

func (m *fileUserManager) SaveUser(ctx context.Context, u DeveloperUser) error {
	store, err := m.loadStore()
	if err != nil {
		return err
	}

	updated := false
	for i, existing := range store.Users {
		if strings.EqualFold(existing.UID, u.UID) || strings.EqualFold(existing.Email, u.Email) {
			store.Users[i] = u
			updated = true
			break
		}
	}

	if !updated {
		store.Users = append(store.Users, u)
	}

	return m.saveStore(store)
}
