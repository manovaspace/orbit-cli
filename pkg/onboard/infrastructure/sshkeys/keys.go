// Package sshkeys detects or generates the developer ed25519 keypair.
package sshkeys

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"golang.org/x/crypto/ssh"
)

// Keys ensures an SSH keypair exists in the configured directory.
type Keys struct {
	Runner runner.Runner
	// LookupEnv defaults to os.Getenv; it decides whether an agent is present.
	LookupEnv func(string) string
}

// New builds Keys over r.
func New(r runner.Runner) *Keys {
	return &Keys{Runner: r}
}

// EnsureKey returns the public key already present in sshDir, or generates a new
// ed25519 keypair (with ssh-keygen when available, Go crypto otherwise) and
// loads it into the running agent.
func (k *Keys) EnsureKey(customSSHDir, email string) (string, error) {
	sshDir := customSSHDir
	if sshDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home dir: %w", err)
		}
		sshDir = filepath.Join(home, ".ssh")
	}

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create ssh directory: %w", err)
	}

	// Reuse an existing key before generating one.
	candidates := []string{
		filepath.Join(sshDir, "id_ed25519.pub"),
		filepath.Join(sshDir, "id_rsa.pub"),
		filepath.Join(sshDir, "id_ecdsa.pub"),
	}
	for _, pubPath := range candidates {
		if data, err := os.ReadFile(pubPath); err == nil {
			keyStr := strings.TrimSpace(string(data))
			if keyStr != "" {
				k.load(context.Background(), strings.TrimSuffix(pubPath, ".pub"))
				return keyStr, nil
			}
		}
	}

	keyPath := filepath.Join(sshDir, "id_ed25519")
	pubKeyPath := keyPath + ".pub"

	comment := email
	if comment == "" {
		comment = "orbit-developer"
	}

	generated := false
	if k.Runner != nil {
		_, err := k.Runner.Run(context.Background(), "", "ssh-keygen", "-t", "ed25519", "-C", comment, "-f", keyPath, "-N", "")
		generated = err == nil
	}

	// Fallback to Go crypto when ssh-keygen is missing or failed.
	if !generated {
		if err := generate(keyPath, pubKeyPath, comment); err != nil {
			return "", err
		}
	}

	data, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read generated public key: %w", err)
	}

	k.load(context.Background(), keyPath)
	return strings.TrimSpace(string(data)), nil
}

// load adds the private key to the agent when one is running.
func (k *Keys) load(ctx context.Context, privKeyPath string) {
	if k.Runner == nil {
		return
	}
	lookup := k.LookupEnv
	if lookup == nil {
		lookup = os.Getenv
	}
	if lookup("SSH_AUTH_SOCK") == "" {
		return
	}
	_, _ = k.Runner.Run(ctx, "", "ssh-add", privKeyPath)
}

// generate writes a fresh ed25519 keypair with the PEM and OpenSSH formats.
func generate(keyPath, pubKeyPath, comment string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	privBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(privBlock), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return fmt.Errorf("failed to format ssh public key: %w", err)
	}
	pubLine := fmt.Sprintf("%s %s\n", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), comment)
	if err := os.WriteFile(pubKeyPath, []byte(pubLine), 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}
	return nil
}
