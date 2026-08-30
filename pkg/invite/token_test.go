package invite

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func testSecret() []byte {
	return []byte("test-super-secret-key-at-least-32-bytes-long!")
}

func TestGenerateAndValidateToken(t *testing.T) {
	req := InviteRequest{
		Email:       "alex@example.com",
		DisplayName: "Alex Smith",
		Scope:       "core",
		TTL:         24 * time.Hour,
		CreatedBy:   "admin@example.com",
		Metadata: map[string]string{
			"role": "engineer",
		},
	}

	tokenStr, claims, err := GenerateToken(req, testSecret())
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Verify token format
	if !strings.HasPrefix(tokenStr, "orbit-inv.") {
		t.Fatalf("expected token prefix 'orbit-inv.', got: %s", tokenStr)
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts in token, got %d parts", len(parts))
	}

	if claims.ID == "" {
		t.Error("expected non-empty claims ID")
	}
	if claims.Email != req.Email {
		t.Errorf("expected email %s, got %s", req.Email, claims.Email)
	}
	if claims.DisplayName != req.DisplayName {
		t.Errorf("expected display name %s, got %s", req.DisplayName, claims.DisplayName)
	}
	if claims.Scope != req.Scope {
		t.Errorf("expected scope %s, got %s", req.Scope, claims.Scope)
	}
	if claims.CreatedBy != req.CreatedBy {
		t.Errorf("expected created_by %s, got %s", req.CreatedBy, claims.CreatedBy)
	}
	if claims.Metadata["role"] != "engineer" {
		t.Errorf("expected metadata role engineer, got %s", claims.Metadata["role"])
	}

	// Validate valid token
	validatedClaims, err := ValidateToken(tokenStr, testSecret())
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if validatedClaims.ID != claims.ID {
		t.Errorf("claims ID mismatch: %s vs %s", validatedClaims.ID, claims.ID)
	}
	if validatedClaims.Email != req.Email || validatedClaims.Scope != req.Scope {
		t.Errorf("claims mismatch: %+v", validatedClaims)
	}
	if validatedClaims.DisplayName != req.DisplayName {
		t.Errorf("claims display name mismatch: %s", validatedClaims.DisplayName)
	}
	if validatedClaims.Metadata["role"] != "engineer" {
		t.Errorf("claims metadata mismatch: %+v", validatedClaims.Metadata)
	}

	// Idempotency key test
	key1 := ComputeIdempotencyKey(tokenStr, "machine-123")
	key2 := ComputeIdempotencyKey(tokenStr, "machine-123")
	if key1 != key2 {
		t.Fatalf("expected deterministic idempotency keys: %s vs %s", key1, key2)
	}
	if len(key1) != 64 {
		t.Errorf("expected SHA-256 hex string (64 chars), got length %d (%s)", len(key1), key1)
	}

	keyDifferentMachine := ComputeIdempotencyKey(tokenStr, "machine-456")
	if key1 == keyDifferentMachine {
		t.Error("expected different idempotency key for different machine ID")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	req := InviteRequest{
		Email:       "expired@example.com",
		DisplayName: "Expired User",
		Scope:       "guest",
		TTL:         -1 * time.Hour, // Expired in the past
	}

	tokenStr, _, err := GenerateToken(req, testSecret())
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ValidateToken(tokenStr, testSecret())
	if err == nil {
		t.Fatalf("expected error for expired token, got valid claims: %+v", claims)
	}

	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestValidateToken_TamperedSignature(t *testing.T) {
	req := InviteRequest{
		Email:       "victim@example.com",
		DisplayName: "Victim",
		Scope:       "core",
		TTL:         1 * time.Hour,
	}

	tokenStr, _, err := GenerateToken(req, testSecret())
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected token structure: %s", tokenStr)
	}

	// 1. Corrupt signature
	tamperedSig := parts[2] + "extra"
	tamperedToken := strings.Join([]string{parts[0], parts[1], tamperedSig}, ".")
	_, err = ValidateToken(tamperedToken, testSecret())
	if err == nil {
		t.Fatal("expected signature verification to fail for corrupted signature")
	}
	if !errors.Is(err, ErrInvalidSignature) && !errors.Is(err, ErrMalformedToken) {
		t.Errorf("expected ErrInvalidSignature or ErrMalformedToken, got: %v", err)
	}

	// 2. Modify payload without updating signature
	tamperedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"attacker@example.com","scope":"admin"}`))
	tamperedToken2 := strings.Join([]string{parts[0], tamperedPayload, parts[2]}, ".")
	_, err = ValidateToken(tamperedToken2, testSecret())
	if err == nil {
		t.Fatal("expected signature verification to fail for tampered payload")
	}
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	req := InviteRequest{
		Email:       "test@example.com",
		DisplayName: "Test",
		Scope:       "core",
		TTL:         1 * time.Hour,
	}

	tokenStr, _, err := GenerateToken(req, testSecret())
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	otherSecret := []byte("completely-different-secret-key-1234567890!")
	_, err = ValidateToken(tokenStr, otherSecret)
	if err == nil {
		t.Fatal("expected error validating token with wrong secret")
	}
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestValidateToken_MalformedFormat(t *testing.T) {
	testCases := []struct {
		name     string
		tokenStr string
	}{
		{"empty string", ""},
		{"single part", "orbit-inv"},
		{"two parts", "orbit-inv.payload"},
		{"four parts", "orbit-inv.part1.part2.part3"},
		{"wrong prefix", "invalid-prefix.cGF5bG9hZA.c2ln"},
		{"invalid base64 payload", "orbit-inv.!!!invalid-base64!!!.c2ln"},
		{"invalid base64 signature", "orbit-inv.eyJlZGFpbCI6InRlc3QifQ.!!!invalid-sig!!!"},
		{"invalid json in payload", func() string {
			badPayload := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
			mac := hmac.New(sha256.New, testSecret())
			mac.Write([]byte(badPayload))
			sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			return "orbit-inv." + badPayload + "." + sig
		}()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateToken(tc.tokenStr, testSecret())
			if err == nil {
				t.Fatalf("expected error for malformed token %q, got nil", tc.tokenStr)
			}
			if !errors.Is(err, ErrMalformedToken) {
				t.Errorf("expected ErrMalformedToken, got: %v", err)
			}
		})
	}
}

func TestGenerateAndValidate_EmptySecret(t *testing.T) {
	req := InviteRequest{
		Email: "test@example.com",
		Scope: "core",
	}

	_, _, err := GenerateToken(req, nil)
	if err == nil {
		t.Error("expected error when generating token with nil secret")
	}

	_, _, err = GenerateToken(req, []byte{})
	if err == nil {
		t.Error("expected error when generating token with empty secret")
	}

	_, err = ValidateToken("orbit-inv.payload.sig", nil)
	if err == nil {
		t.Error("expected error when validating token with nil secret")
	}

	_, err = ValidateToken("orbit-inv.payload.sig", []byte{})
	if err == nil {
		t.Error("expected error when validating token with empty secret")
	}
}
