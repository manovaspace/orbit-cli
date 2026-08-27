package owner

import (
	"errors"
	"time"
)

var (
	// ErrOwnerNotFound indicates that no owner record was found.
	ErrOwnerNotFound = errors.New("owner record not found")

	// ErrUnverifiedOwner indicates that the owner has not completed verification.
	ErrUnverifiedOwner = errors.New("platform owner is unverified")

	// ErrInvalidOwnerRecord indicates that the owner record data is malformed or invalid.
	ErrInvalidOwnerRecord = errors.New("invalid owner record")

	// ErrInsecurePermissions indicates that the owner storage file has overly permissive permissions.
	ErrInsecurePermissions = errors.New("insecure owner file permissions (must be 0600)")

	// ErrChallengeNotFound indicates that no active OTP challenge exists for the given email.
	ErrChallengeNotFound = errors.New("no active challenge found for email")

	// ErrChallengeExpired indicates that the OTP challenge has expired.
	ErrChallengeExpired = errors.New("verification challenge has expired")

	// ErrMaxAttemptsExceeded indicates that maximum verification attempts have been reached.
	ErrMaxAttemptsExceeded = errors.New("maximum verification attempts exceeded")

	// ErrInvalidCode indicates that the provided verification code does not match.
	ErrInvalidCode = errors.New("invalid verification code")
)

// OwnerRecord represents the persistent cryptographic identity of the platform/server owner.
type OwnerRecord struct {
	Email             string    `json:"email"`
	DisplayName       string    `json:"display_name,omitempty"`
	VerifiedAt        time.Time `json:"verified_at"`
	RootSigningSecret string    `json:"root_signing_secret"`
	KeyFingerprint    string    `json:"key_fingerprint,omitempty"`
}

// IsVerified returns true if the owner record is non-nil, has a non-empty email,
// non-zero verified timestamp, and a non-empty root signing secret.
func (r *OwnerRecord) IsVerified() bool {
	if r == nil {
		return false
	}
	return r.Email != "" && !r.VerifiedAt.IsZero() && r.RootSigningSecret != ""
}
