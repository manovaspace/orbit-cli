package provisioner

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// ForgejoAccount models the internal Forgejo profile state for a provisioned developer.
type ForgejoAccount struct {
	Username string
	SSHKey   string
	Token    string
}

// WireGuardPeer models the allocated WireGuard VPN peer configuration.
type WireGuardPeer struct {
	UID       string
	IP        string
	Config    string
	PublicKey string
}

// DevProvisioner is an in-memory, deterministic implementation of Provisioner
// designed for local dev stacks, testing, and isolated staging environments.
type DevProvisioner struct {
	mu             sync.RWMutex
	users          map[string]*ClaimResponse
	userByEmail    map[string]string
	lldapUsers     map[string]User
	forgejoUsers   map[string]ForgejoAccount
	wireguardPeers map[string]WireGuardPeer
	allocatedIPs   map[int]string

	GitRemoteBase      string
	DefaultScope       string
	WireGuardEndpoint  string
	WireGuardDNS       string
	WireGuardSubnet    string
	WireGuardServerPub string

	// Error simulation hooks for atomic rollback testing
	SimulateLldapError     error
	SimulateForgejoError   error
	SimulateWireGuardError error
	SimulateHealthError    error
	OnRollback             func(stage, uid string)
}

// NewDevProvisioner initializes a new DevProvisioner with production-like defaults.
func NewDevProvisioner() *DevProvisioner {
	return &DevProvisioner{
		users:              make(map[string]*ClaimResponse),
		userByEmail:        make(map[string]string),
		lldapUsers:         make(map[string]User),
		forgejoUsers:       make(map[string]ForgejoAccount),
		wireguardPeers:     make(map[string]WireGuardPeer),
		allocatedIPs:       make(map[int]string),
		GitRemoteBase:      "ssh://git@git.dev.manova.space/manova",
		DefaultScope:       "core",
		WireGuardEndpoint:  "vpn.dev.manova.space:51820",
		WireGuardDNS:       "10.8.0.1",
		WireGuardSubnet:    "10.8.0.0/24",
		WireGuardServerPub: "k8+8fW51TqJz9wQf+18/fR2XpL2kL5Lw8K3P6R7M=",
	}
}

// Provision handles full atomic provisioning of user identity, Forgejo credentials, and VPN profile.
func (d *DevProvisioner) Provision(ctx context.Context, req ClaimRequest) (resp *ClaimResponse, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	uid := strings.TrimSpace(req.DesiredUID)
	if uid == "" && req.Email != "" {
		parts := strings.Split(req.Email, "@")
		uid = sanitizeUID(parts[0])
	}
	if uid == "" {
		return nil, fmt.Errorf("%w: missing desired_uid and email", ErrInvalidRequest)
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = fmt.Sprintf("%s@manova.space", uid)
	}

	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = d.DefaultScope
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = uid
	}

	// Idempotency check: if user already provisioned, return cached credentials
	if existing, ok := d.users[uid]; ok {
		cachedCopy := *existing
		cachedCopy.IdempotentReplay = true
		return &cachedCopy, nil
	}
	if existingUID, ok := d.userByEmail[email]; ok {
		if existing, ok := d.users[existingUID]; ok {
			cachedCopy := *existing
			cachedCopy.IdempotentReplay = true
			return &cachedCopy, nil
		}
	}

	// Rollback stack for transactional safety
	var rollbacks []func()
	defer func() {
		if err != nil {
			for i := len(rollbacks) - 1; i >= 0; i-- {
				rollbacks[i]()
			}
		}
	}()

	// Stage 1: LLDAP Identity Provisioning
	if d.SimulateLldapError != nil {
		return nil, fmt.Errorf("%w: lldap step failed: %w", ErrProvisionFailed, d.SimulateLldapError)
	}

	groups := []string{"dev"}
	if scope == "core" {
		groups = append(groups, "orbit")
	} else if scope != "" {
		groups = append(groups, scope)
	}

	d.lldapUsers[uid] = User{
		UID:         uid,
		Email:       email,
		DisplayName: displayName,
		Groups:      groups,
	}
	rollbacks = append(rollbacks, func() {
		delete(d.lldapUsers, uid)
		if d.OnRollback != nil {
			d.OnRollback("lldap", uid)
		}
	})

	// Stage 2: Forgejo User, SSH Key & MCP Token Provisioning
	if d.SimulateForgejoError != nil {
		return nil, fmt.Errorf("%w: forgejo step failed: %w", ErrProvisionFailed, d.SimulateForgejoError)
	}

	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	mcpToken := "fjo_tok_" + hex.EncodeToString(tokenBytes)

	d.forgejoUsers[uid] = ForgejoAccount{
		Username: uid,
		SSHKey:   req.SSHPublicKey,
		Token:    mcpToken,
	}
	rollbacks = append(rollbacks, func() {
		delete(d.forgejoUsers, uid)
		if d.OnRollback != nil {
			d.OnRollback("forgejo", uid)
		}
	})

	// Stage 3: WireGuard VPN Peer Allocation
	if d.SimulateWireGuardError != nil {
		return nil, fmt.Errorf("%w: wireguard step failed: %w", ErrProvisionFailed, d.SimulateWireGuardError)
	}

	ipOffset := d.allocateFreeIPOffset()
	allocatedIP := fmt.Sprintf("10.8.0.%d/24", ipOffset)
	d.allocatedIPs[ipOffset] = uid

	privKeyBytes := make([]byte, 32)
	_, _ = rand.Read(privKeyBytes)
	clientPrivKey := base64.StdEncoding.EncodeToString(privKeyBytes)

	wgConfig := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = %s

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = 10.8.0.0/16
`, clientPrivKey, allocatedIP, d.WireGuardDNS, d.WireGuardServerPub, d.WireGuardEndpoint)

	d.wireguardPeers[uid] = WireGuardPeer{
		UID:       uid,
		IP:        allocatedIP,
		Config:    wgConfig,
		PublicKey: clientPrivKey,
	}
	rollbacks = append(rollbacks, func() {
		delete(d.wireguardPeers, uid)
		delete(d.allocatedIPs, ipOffset)
		if d.OnRollback != nil {
			d.OnRollback("wireguard", uid)
		}
	})

	// Final Response Assembly
	finalResp := &ClaimResponse{
		Status:           "success",
		IdempotentReplay: false,
		User: User{
			UID:         uid,
			Email:       email,
			DisplayName: displayName,
			Groups:      groups,
		},
		Credentials: Credentials{
			ForgejoUsername: uid,
			ForgejoMCPToken: mcpToken,
			WireGuardConfig: wgConfig,
		},
		Workspace: WorkspaceInfo{
			GitRemoteBase:        d.GitRemoteBase,
			DefaultManifestScope: scope,
		},
	}

	d.users[uid] = finalResp
	d.userByEmail[email] = uid

	return finalResp, nil
}

func (d *DevProvisioner) allocateFreeIPOffset() int {
	for i := 10; i < 254; i++ {
		if _, exists := d.allocatedIPs[i]; !exists {
			return i
		}
	}
	return 254
}

// Rollback rolls back and deletes all provisioned resources for a specific user ID.
func (d *DevProvisioner) Rollback(ctx context.Context, uid string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.users, uid)
	delete(d.lldapUsers, uid)
	delete(d.forgejoUsers, uid)
	delete(d.wireguardPeers, uid)

	// Clean email index
	for email, u := range d.userByEmail {
		if u == uid {
			delete(d.userByEmail, email)
			break
		}
	}

	// Clean allocated IP
	for offset, u := range d.allocatedIPs {
		if u == uid {
			delete(d.allocatedIPs, offset)
			break
		}
	}

	if d.OnRollback != nil {
		d.OnRollback("all", uid)
	}

	return nil
}

// Health checks the health of the provisioner.
func (d *DevProvisioner) Health(ctx context.Context) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.SimulateHealthError != nil {
		return d.SimulateHealthError
	}
	return nil
}

// Helper inspection methods for tests
func (d *DevProvisioner) HasUser(uid string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.users[uid]
	return ok
}

func (d *DevProvisioner) HasLLDAPUser(uid string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.lldapUsers[uid]
	return ok
}

func (d *DevProvisioner) HasForgejoUser(uid string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.forgejoUsers[uid]
	return ok
}

func (d *DevProvisioner) HasWireGuardPeer(uid string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.wireguardPeers[uid]
	return ok
}

func sanitizeUID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var sb strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// MockProvisioner is a versatile mock for unit tests requiring precise provisioner control.
type MockProvisioner struct {
	mu             sync.RWMutex
	ProvisionCalls []ClaimRequest
	RollbackCalls  []string
	HealthCalls    int

	ProvisionFunc func(ctx context.Context, req ClaimRequest) (*ClaimResponse, error)
	RollbackFunc  func(ctx context.Context, uid string) error
	HealthFunc    func(ctx context.Context) error
}

// NewMockProvisioner returns a MockProvisioner with sensible default mock behavior.
func NewMockProvisioner() *MockProvisioner {
	return &MockProvisioner{
		ProvisionCalls: make([]ClaimRequest, 0),
		RollbackCalls:  make([]string, 0),
	}
}

func (m *MockProvisioner) Provision(ctx context.Context, req ClaimRequest) (*ClaimResponse, error) {
	m.mu.Lock()
	m.ProvisionCalls = append(m.ProvisionCalls, req)
	customFunc := m.ProvisionFunc
	m.mu.Unlock()

	if customFunc != nil {
		return customFunc(ctx, req)
	}

	uid := req.DesiredUID
	if uid == "" {
		uid = "mock-user"
	}
	email := req.Email
	if email == "" {
		email = uid + "@manova.space"
	}

	return &ClaimResponse{
		Status:           "success",
		IdempotentReplay: false,
		User: User{
			UID:         uid,
			Email:       email,
			DisplayName: req.DisplayName,
			Groups:      []string{"dev", "mock"},
		},
		Credentials: Credentials{
			ForgejoUsername: uid,
			ForgejoMCPToken: "fjo_tok_mock_token_123456",
			WireGuardConfig: "[Interface]\nAddress = 10.8.0.99/24\n",
		},
		Workspace: WorkspaceInfo{
			GitRemoteBase:        "ssh://git@git.dev.manova.space/manova",
			DefaultManifestScope: "core",
		},
	}, nil
}

func (m *MockProvisioner) Rollback(ctx context.Context, uid string) error {
	m.mu.Lock()
	m.RollbackCalls = append(m.RollbackCalls, uid)
	customFunc := m.RollbackFunc
	m.mu.Unlock()

	if customFunc != nil {
		return customFunc(ctx, uid)
	}
	return nil
}

func (m *MockProvisioner) Health(ctx context.Context) error {
	m.mu.Lock()
	m.HealthCalls++
	customFunc := m.HealthFunc
	m.mu.Unlock()

	if customFunc != nil {
		return customFunc(ctx)
	}
	return nil
}
