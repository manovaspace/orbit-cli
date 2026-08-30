package invite

import (
	"fmt"
	"strings"
	"time"

	"github.com/manovaspace/orbit-notifications/pkg/mailtemplates"
)

// EmailData holds template variables for developer onboarding invitation emails.
type EmailData struct {
	RecipientName  string
	RecipientEmail string
	Token          string
	ShortCode      string
	Scope          string
	ExpiresAt      time.Time
	ExpiresInHuman string
	CLICommand     string
	CurlCommand    string
}

// FormatRemaining formats a remaining duration into human-readable text.
func FormatRemaining(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	if d > 24*time.Hour {
		days := int(d.Hours() / 24)
		hours := int(d.Hours()) % 24
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if d > time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// RenderInviteEmail renders both text and HTML versions of the invitation email.
func RenderInviteEmail(data EmailData) (subject, textBody, htmlBody string, err error) {
	if data.CLICommand == "" {
		data.CLICommand = fmt.Sprintf("orbit onboard --token %s", data.Token)
	}
	if data.CurlCommand == "" {
		data.CurlCommand = fmt.Sprintf("curl -fsSL https://orbit.manova.space | bash -s -- onboard --token %s", data.Token)
	}
	if data.ExpiresInHuman == "" && !data.ExpiresAt.IsZero() {
		data.ExpiresInHuman = FormatRemaining(time.Until(data.ExpiresAt))
	}

	expiresAt := ""
	if !data.ExpiresAt.IsZero() {
		expiresAt = data.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC")
	}

	vars := map[string]string{
		"name":         strings.TrimSpace(data.RecipientName),
		"token":        data.Token,
		"cli_command":  data.CLICommand,
		"curl_command": data.CurlCommand,
		"expires_in":   data.ExpiresInHuman,
		"expires_at":   expiresAt,
	}

	res, err := mailtemplates.Render("invite_developer", vars)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to render invite email: %w", err)
	}
	return res.Subject, res.Text, res.HTML, nil
}

// OwnerChallengeEmailData holds template variables for owner verification OTP challenge emails.
type OwnerChallengeEmailData struct {
	OwnerEmail  string
	OTPCode     string
	ExpiresIn   string
	ServerHost  string
	GeneratedAt time.Time
}

// RenderOwnerChallengeEmail renders both plaintext and HTML versions of the server ownership challenge email.
func RenderOwnerChallengeEmail(data OwnerChallengeEmailData) (subject, textBody, htmlBody string, err error) {
	otp := strings.TrimSpace(data.OTPCode)
	if otp == "" {
		return "", "", "", fmt.Errorf("OTP code cannot be empty")
	}

	if data.ExpiresIn == "" {
		data.ExpiresIn = "10 minutes"
	}
	if data.GeneratedAt.IsZero() {
		data.GeneratedAt = time.Now().UTC()
	}

	vars := map[string]string{
		"code":         otp,
		"email":        data.OwnerEmail,
		"expires_in":   data.ExpiresIn,
		"server_host":  data.ServerHost,
		"generated_at": data.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
	}

	res, err := mailtemplates.Render("owner_challenge", vars)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to render owner challenge email: %w", err)
	}
	return res.Subject, res.Text, res.HTML, nil
}
