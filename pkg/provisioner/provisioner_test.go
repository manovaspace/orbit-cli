package provisioner

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDevProvisioner_Success(t *testing.T) {
	ctx := context.Background()
	p := NewDevProvisioner()

	req := ClaimRequest{
		InviteToken:        "manova-inv.test.sig",
		DesiredUID:         "alex",
		Email:              "alex@manova.space",
		DisplayName:        "Alex Smith",
		SSHPublicKey:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG... alex@laptop",
		MachineFingerprint: "fingerprint-123",
		Scope:              "core",
	}

	resp, err := p.Provision(ctx, req)
	if err != nil {
		t.Fatalf("unexpected provision error: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("expected status 'success', got %q", resp.Status)
	}
	if resp.IdempotentReplay {
		t.Errorf("expected IdempotentReplay to be false on first claim")
	}
	if resp.User.UID != "alex" {
		t.Errorf("expected UID 'alex', got %q", resp.User.UID)
	}
	if resp.User.Email != "alex@manova.space" {
		t.Errorf("expected email 'alex@manova.space', got %q", resp.User.Email)
	}
	if len(resp.User.Groups) == 0 {
		t.Errorf("expected user groups to be non-empty")
	}
	if resp.Credentials.ForgejoUsername != "alex" {
		t.Errorf("expected ForgejoUsername 'alex', got %q", resp.Credentials.ForgejoUsername)
	}
	if !strings.HasPrefix(resp.Credentials.ForgejoMCPToken, "fjo_tok_") {
		t.Errorf("expected MCP token prefix 'fjo_tok_', got %q", resp.Credentials.ForgejoMCPToken)
	}
	if !strings.Contains(resp.Credentials.WireGuardConfig, "[Interface]") {
		t.Errorf("expected WireGuard config with [Interface], got %q", resp.Credentials.WireGuardConfig)
	}
	if resp.Workspace.GitRemoteBase == "" {
		t.Errorf("expected non-empty GitRemoteBase")
	}
	if resp.Workspace.DefaultManifestScope != "core" {
		t.Errorf("expected DefaultManifestScope 'core', got %q", resp.Workspace.DefaultManifestScope)
	}

	// Health should pass
	if err := p.Health(ctx); err != nil {
		t.Errorf("expected Health to pass, got: %v", err)
	}
}

func TestDevProvisioner_IdempotentReplay(t *testing.T) {
	ctx := context.Background()
	p := NewDevProvisioner()

	req := ClaimRequest{
		InviteToken:        "manova-inv.test.sig",
		DesiredUID:         "sam",
		Email:              "sam@manova.space",
		DisplayName:        "Sam Taylor",
		SSHPublicKey:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG... sam@laptop",
		MachineFingerprint: "fingerprint-456",
		Scope:              "client",
	}

	resp1, err := p.Provision(ctx, req)
	if err != nil {
		t.Fatalf("first provision failed: %v", err)
	}
	if resp1.IdempotentReplay {
		t.Errorf("expected first call IdempotentReplay == false")
	}

	// Second claim with same user
	resp2, err := p.Provision(ctx, req)
	if err != nil {
		t.Fatalf("second provision failed: %v", err)
	}
	if !resp2.IdempotentReplay {
		t.Errorf("expected second call IdempotentReplay == true")
	}
	if resp2.Credentials.ForgejoMCPToken != resp1.Credentials.ForgejoMCPToken {
		t.Errorf("expected identical credentials on replay, got %s vs %s",
			resp2.Credentials.ForgejoMCPToken, resp1.Credentials.ForgejoMCPToken)
	}
	if resp2.Credentials.WireGuardConfig != resp1.Credentials.WireGuardConfig {
		t.Errorf("expected identical wireguard config on replay")
	}
}

func TestDevProvisioner_AtomicRollback_OnLldapFailure(t *testing.T) {
	ctx := context.Background()
	p := NewDevProvisioner()

	simErr := errors.New("simulated lldap LDAP connection refused")
	p.SimulateLldapError = simErr

	req := ClaimRequest{
		InviteToken:  "manova-inv.test.sig",
		DesiredUID:   "failuser",
		Email:        "failuser@manova.space",
		SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...",
	}

	_, err := p.Provision(ctx, req)
	if err == nil {
		t.Fatal("expected provision to fail when Lldap error is simulated")
	}
	if !errors.Is(err, simErr) && !strings.Contains(err.Error(), simErr.Error()) {
		t.Errorf("expected error to contain simulated error, got: %v", err)
	}

	// Verify no residual state
	if p.HasUser("failuser") {
		t.Errorf("expected failuser to not exist after rollback")
	}
}

func TestDevProvisioner_AtomicRollback_OnForgejoFailure(t *testing.T) {
	ctx := context.Background()
	p := NewDevProvisioner()

	var rolledBackStages []string
	p.OnRollback = func(stage, uid string) {
		rolledBackStages = append(rolledBackStages, stage)
	}

	simErr := errors.New("simulated forgejo API 500 internal server error")
	p.SimulateForgejoError = simErr

	req := ClaimRequest{
		InviteToken:  "manova-inv.test.sig",
		DesiredUID:   "forgejofail",
		Email:        "forgejofail@manova.space",
		SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...",
	}

	_, err := p.Provision(ctx, req)
	if err == nil {
		t.Fatal("expected provision to fail when Forgejo error is simulated")
	}

	// LLDAP stage should have been rolled back
	if p.HasUser("forgejofail") {
		t.Errorf("expected forgejofail to not exist after rollback")
	}
	if p.HasLLDAPUser("forgejofail") {
		t.Errorf("expected lldap record for forgejofail to be rolled back")
	}

	foundLldapRollback := false
	for _, st := range rolledBackStages {
		if st == "lldap" {
			foundLldapRollback = true
		}
	}
	if !foundLldapRollback {
		t.Errorf("expected lldap rollback hook to have been triggered, got %v", rolledBackStages)
	}
}

func TestDevProvisioner_AtomicRollback_OnWireGuardFailure(t *testing.T) {
	ctx := context.Background()
	p := NewDevProvisioner()

	var rolledBackStages []string
	p.OnRollback = func(stage, uid string) {
		rolledBackStages = append(rolledBackStages, stage)
	}

	simErr := errors.New("simulated wireguard IP exhaustion")
	p.SimulateWireGuardError = simErr

	req := ClaimRequest{
		InviteToken:  "manova-inv.test.sig",
		DesiredUID:   "wgfail",
		Email:        "wgfail@manova.space",
		SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...",
	}

	_, err := p.Provision(ctx, req)
	if err == nil {
		t.Fatal("expected provision to fail when WireGuard error is simulated")
	}

	if p.HasUser("wgfail") || p.HasLLDAPUser("wgfail") || p.HasForgejoUser("wgfail") {
		t.Errorf("expected complete rollback of all stages for wgfail")
	}

	hasLldap := false
	hasForgejo := false
	for _, st := range rolledBackStages {
		if st == "lldap" {
			hasLldap = true
		}
		if st == "forgejo" {
			hasForgejo = true
		}
	}
	if !hasLldap || !hasForgejo {
		t.Errorf("expected both lldap and forgejo rollbacks, got: %v", rolledBackStages)
	}
}

func TestDevProvisioner_ManualRollback(t *testing.T) {
	ctx := context.Background()
	p := NewDevProvisioner()

	req := ClaimRequest{
		InviteToken:  "manova-inv.test.sig",
		DesiredUID:   "cleanup-user",
		Email:        "cleanup@manova.space",
		SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...",
	}

	_, err := p.Provision(ctx, req)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	if !p.HasUser("cleanup-user") {
		t.Fatal("expected user to exist after provisioning")
	}

	if err := p.Rollback(ctx, "cleanup-user"); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	if p.HasUser("cleanup-user") {
		t.Fatal("expected user to be removed after rollback")
	}
}

func TestDevProvisioner_Health(t *testing.T) {
	ctx := context.Background()
	p := NewDevProvisioner()

	if err := p.Health(ctx); err != nil {
		t.Errorf("expected health to pass initially, got %v", err)
	}

	healthErr := errors.New("upstream service connection lost")
	p.SimulateHealthError = healthErr

	if err := p.Health(ctx); !errors.Is(err, healthErr) {
		t.Errorf("expected simulated health error, got %v", err)
	}
}

func TestMockProvisioner(t *testing.T) {
	ctx := context.Background()
	mock := NewMockProvisioner()

	req := ClaimRequest{
		InviteToken: "manova-inv.test",
		DesiredUID:  "mockuser",
		Email:       "mock@manova.space",
	}

	resp, err := mock.Provision(ctx, req)
	if err != nil {
		t.Fatalf("mock provision failed: %v", err)
	}
	if resp.User.UID != "mockuser" {
		t.Errorf("expected UID mockuser, got %s", resp.User.UID)
	}

	if len(mock.ProvisionCalls) != 1 {
		t.Errorf("expected 1 recorded provision call, got %d", len(mock.ProvisionCalls))
	}

	if err := mock.Rollback(ctx, "mockuser"); err != nil {
		t.Fatalf("mock rollback failed: %v", err)
	}
	if len(mock.RollbackCalls) != 1 || mock.RollbackCalls[0] != "mockuser" {
		t.Errorf("expected recorded rollback call for mockuser, got %v", mock.RollbackCalls)
	}

	if err := mock.Health(ctx); err != nil {
		t.Fatalf("mock health failed: %v", err)
	}
	if mock.HealthCalls != 1 {
		t.Errorf("expected 1 recorded health call, got %d", mock.HealthCalls)
	}
}
