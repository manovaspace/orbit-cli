package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateMasterSecret generates a 32-byte cryptographically secure random secret string (64 hex characters).
func GenerateMasterSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate master secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ComputeFingerprint calculates a SHA-256 fingerprint representation of a secret.
func ComputeFingerprint(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:16])
}
