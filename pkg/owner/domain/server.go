package domain

import "time"

// ChallengeDispatch is the server acknowledgement of an owner challenge.
type ChallengeDispatch struct {
	Email string
}

// Verification is the server's answer to an OTP submission.
type Verification struct {
	Status string
}

// GrantRegistration is the server's record of a registered admin grant.
type GrantRegistration struct {
	ExpiresAt time.Time
}
