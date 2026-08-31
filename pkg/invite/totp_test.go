package invite

import (
	"strings"
	"testing"
)

func TestGenerateTOTP(t *testing.T) {
	secret, uri, err := GenerateTOTP("Orbit", "alireza@manova.space")
	if err != nil {
		t.Fatalf("GenerateTOTP failed: %v", err)
	}

	if len(secret) != 32 { // 20 bytes * 8 / 5 = 32 base32 characters
		t.Errorf("expected 32-character base32 secret, got %d chars (%q)", len(secret), secret)
	}

	if !strings.HasPrefix(uri, "otpauth://totp/Orbit:alireza@manova.space?") {
		t.Errorf("unexpected uri prefix: %q", uri)
	}

	if !strings.Contains(uri, "secret="+secret) {
		t.Errorf("uri should contain secret: %q", uri)
	}

	if !strings.Contains(uri, "issuer=Orbit") {
		t.Errorf("uri should contain issuer=Orbit: %q", uri)
	}
}
