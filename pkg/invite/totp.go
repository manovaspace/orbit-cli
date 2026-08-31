package invite

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/url"
	"strings"
)

// GenerateTOTP generates a random 20-byte base32 secret and an otpauth:// URI compliant with RFC 6238.
func GenerateTOTP(issuer, account string) (secret string, uri string, err error) {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = "Orbit"
	}
	account = strings.TrimSpace(account)
	if account == "" {
		account = "developer"
	}

	randomBytes := make([]byte, 20)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes for TOTP: %w", err)
	}

	secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes)
	label := fmt.Sprintf("%s:%s", issuer, account)
	uri = fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		url.PathEscape(label),
		url.QueryEscape(secret),
		url.QueryEscape(issuer),
	)

	return secret, uri, nil
}
