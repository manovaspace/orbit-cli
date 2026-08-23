package invite

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const TokenPrefix = "manova-inv"

// ComputeIdempotencyKey computes a deterministic SHA-256 idempotency key from a token and machine ID.
func ComputeIdempotencyKey(tokenStr, machineID string) string {
	h := sha256.New()
	h.Write([]byte(tokenStr + ":" + machineID))
	return hex.EncodeToString(h.Sum(nil))
}

// GenerateToken generates a cryptographically signed token for an invite request.
// Token format: manova-inv.<payloadB64>.<sigB64>
func GenerateToken(req InviteRequest, secret []byte) (string, *InviteClaims, error) {
	if len(secret) == 0 {
		return "", nil, errors.New("secret key cannot be empty")
	}

	ttl := req.TTL
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}

	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return "", nil, fmt.Errorf("failed to generate token ID: %w", err)
	}

	now := time.Now().UTC()
	claims := &InviteClaims{
		ID:          hex.EncodeToString(rawID),
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Scope:       req.Scope,
		IssuedAt:    now,
		ExpiresAt:   now.Add(ttl),
		CreatedBy:   req.CreatedBy,
		Metadata:    req.Metadata,
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal token claims: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payloadB64))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	tokenStr := fmt.Sprintf("%s.%s.%s", TokenPrefix, payloadB64, sigB64)
	return tokenStr, claims, nil
}

// ValidateToken validates a token string against the secret, checks HMAC signature and expiry, and returns claims.
func ValidateToken(tokenStr string, secret []byte) (*InviteClaims, error) {
	if len(secret) == 0 {
		return nil, errors.New("secret key cannot be empty")
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 dot-separated parts, got %d", ErrMalformedToken, len(parts))
	}

	if parts[0] != TokenPrefix {
		return nil, fmt.Errorf("%w: invalid prefix %q", ErrMalformedToken, parts[0])
	}

	payloadBytes, err := decodeBase64(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload base64: %w", ErrMalformedToken, err)
	}

	sigBytes, err := decodeBase64(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature base64: %w", ErrMalformedToken, err)
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[1]))
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expectedSig) {
		return nil, ErrInvalidSignature
	}

	var claims InviteClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("%w: invalid claims JSON: %w", ErrMalformedToken, err)
	}

	if claims.IsExpired() {
		return nil, ErrTokenExpired
	}

	return &claims, nil
}

func decodeBase64(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}
