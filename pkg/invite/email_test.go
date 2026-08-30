package invite

import (
	"strings"
	"testing"
	"time"
)

func TestRenderInviteEmail(t *testing.T) {
	data := EmailData{
		RecipientName:  "Alex Smith",
		RecipientEmail: "alex@manova.space",
		Token:          "9x7k2m4p",
		ShortCode:      "9x7k2m4p",
		Scope:          "core",
		ExpiresAt:      time.Now().Add(7 * 24 * time.Hour).UTC(),
		ExpiresInHuman: "7 days",
		CLICommand:     "orbit onboard --token 9x7k2m4p",
		CurlCommand:    "curl -fsSL https://orbit.manova.space | bash -s -- onboard --token 9x7k2m4p",
	}

	subject, textBody, htmlBody, err := RenderInviteEmail(data)
	if err != nil {
		t.Fatalf("RenderInviteEmail returned unexpected error: %v", err)
	}

	if subject != "Welcome to Manova — developer workspace invitation" {
		t.Errorf("expected exact subject, got %q", subject)
	}
	if strings.Contains(subject, data.Token) {
		t.Fatal("subject leaked token")
	}

	if !strings.Contains(textBody, "Alex Smith") {
		t.Errorf("textBody missing recipient name: %s", textBody)
	}
	if !strings.Contains(textBody, "9x7k2m4p") {
		t.Errorf("textBody missing token: %s", textBody)
	}
	if !strings.Contains(textBody, data.CLICommand) {
		t.Errorf("textBody missing CLI command: %s", textBody)
	}

	if !strings.Contains(htmlBody, "Alex Smith") {
		t.Errorf("htmlBody missing recipient name: %s", htmlBody)
	}
	if !strings.Contains(htmlBody, "9x7k2m4p") {
		t.Errorf("htmlBody missing token: %s", htmlBody)
	}
	if !strings.Contains(htmlBody, data.CLICommand) {
		t.Errorf("htmlBody missing CLI command: %s", htmlBody)
	}
	if !strings.Contains(htmlBody, "#fafafa") || !strings.Contains(htmlBody, "#2563eb") {
		t.Errorf("htmlBody missing expected styling: %s", htmlBody)
	}
	if strings.Contains(htmlBody, "#09090b") {
		t.Fatal("htmlBody still uses zinc dark palette")
	}
}

func TestRenderInviteEmail_FallbackName(t *testing.T) {
	data := EmailData{
		RecipientEmail: "developer@manova.space",
		Token:          "K8mP2x9L",
		ExpiresAt:      time.Now().Add(24 * time.Hour),
		ExpiresInHuman: "24 hours",
	}

	_, textBody, htmlBody, err := RenderInviteEmail(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(textBody, "Developer") {
		t.Errorf("expected fallback greeting in textBody: %s", textBody)
	}
	if !strings.Contains(htmlBody, "Developer") {
		t.Errorf("expected fallback greeting in htmlBody: %s", htmlBody)
	}
}

func TestRenderOwnerChallengeEmail(t *testing.T) {
	genTime := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	data := OwnerChallengeEmailData{
		OwnerEmail:  "alirezaopmc@gmail.com",
		OTPCode:     "749102",
		ExpiresIn:   "10 minutes",
		ServerHost:  "mail.manova.space",
		GeneratedAt: genTime,
	}

	subject, textBody, htmlBody, err := RenderOwnerChallengeEmail(data)
	if err != nil {
		t.Fatalf("RenderOwnerChallengeEmail returned unexpected error: %v", err)
	}

	if subject != "Orbit — server ownership verification" {
		t.Errorf("expected subject %q, got %q", "Orbit — server ownership verification", subject)
	}
	if strings.Contains(subject, "749102") {
		t.Fatal("subject leaked OTP code")
	}

	if !strings.Contains(textBody, "749102") {
		t.Errorf("textBody missing OTP code: %s", textBody)
	}
	if !strings.Contains(textBody, "alirezaopmc@gmail.com") {
		t.Errorf("textBody missing owner email: %s", textBody)
	}
	if !strings.Contains(textBody, "10 minutes") {
		t.Errorf("textBody missing expiry: %s", textBody)
	}
	if !strings.Contains(textBody, "mail.manova.space") {
		t.Errorf("textBody missing server host: %s", textBody)
	}

	if !strings.Contains(htmlBody, "749102") {
		t.Errorf("htmlBody missing OTP code: %s", htmlBody)
	}
	if !strings.Contains(htmlBody, "alirezaopmc@gmail.com") {
		t.Errorf("htmlBody missing owner email: %s", htmlBody)
	}
	if !strings.Contains(htmlBody, "#fafafa") || !strings.Contains(htmlBody, "#2563eb") {
		t.Errorf("htmlBody missing expected styling: %s", htmlBody)
	}
	if strings.Contains(htmlBody, "#09090b") {
		t.Fatal("htmlBody still uses zinc dark palette")
	}
}

func TestRenderOwnerChallengeEmail_Defaults(t *testing.T) {
	data := OwnerChallengeEmailData{
		OwnerEmail: "owner@manova.space",
		OTPCode:    "555123",
	}

	subject, textBody, htmlBody, err := RenderOwnerChallengeEmail(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(subject, "555123") {
		t.Errorf("subject must not contain OTP code, got %q", subject)
	}
	if !strings.Contains(textBody, "10 minutes") {
		t.Errorf("expected default 10 minutes in textBody: %s", textBody)
	}
	if !strings.Contains(htmlBody, "10 minutes") {
		t.Errorf("expected default 10 minutes in htmlBody: %s", htmlBody)
	}
}

func TestRenderOwnerChallengeEmail_EmptyCode(t *testing.T) {
	data := OwnerChallengeEmailData{
		OwnerEmail: "owner@manova.space",
		OTPCode:    "",
	}

	_, _, _, err := RenderOwnerChallengeEmail(data)
	if err == nil {
		t.Fatal("expected error for empty OTPCode, got nil")
	}
}
