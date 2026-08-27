package invite

import (
	"bytes"
	"fmt"
	htmltmpl "html/template"
	"strings"
	texttmpl "text/template"
	"time"
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

const plainTextTemplate = `Welcome to Manova, {{if .RecipientName}}{{.RecipientName}}{{else}}Developer{{end}}!

You have been invited to join the Manova engineering workspace.

================================================================================
YOUR ONBOARDING CLAIM DETAILS
================================================================================

Account Email: {{.RecipientEmail}}
Access Scope:  {{if .Scope}}{{.Scope}}{{else}}core{{end}}
Claim Token:   {{.Token}}
Expires In:    {{.ExpiresInHuman}} ({{.ExpiresAt.Format "2006-01-02 15:04:05 UTC"}})

--------------------------------------------------------------------------------
QUICK START (1-STEP ONBOARDING)
--------------------------------------------------------------------------------

If you already have the Orbit CLI installed:
  {{.CLICommand}}

If setting up a fresh developer workstation:
  {{.CurlCommand}}

--------------------------------------------------------------------------------
WHAT GETS CONFIGURED AUTOMATICALLY:
--------------------------------------------------------------------------------
  • Git (Forgejo) developer account & SSH key registration
  • WireGuard VPN developer profile & tunnel credentials
  • LLDAP single sign-on identity
  • Cursor IDE configuration & MCP platform integrations
  • Multi-repository workspace synchronization

If you did not expect this invitation, please contact security@manova.space.
--
Manova Platform Engineering Team
`

const htmlEmailTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Welcome to Manova — Developer Workspace Invitation</title>
  <style>
    body {
      margin: 0;
      padding: 0;
      background-color: #09090b;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
      color: #e4e4e7;
      line-height: 1.6;
    }
    .wrapper {
      max-width: 600px;
      margin: 0 auto;
      padding: 32px 20px;
    }
    .brand-header {
      text-align: center;
      padding-bottom: 24px;
    }
    .brand-logo {
      font-size: 20px;
      font-weight: 700;
      letter-spacing: 0.05em;
      color: #ffffff;
      text-transform: uppercase;
    }
    .brand-badge {
      display: inline-block;
      font-size: 11px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      padding: 3px 10px;
      border-radius: 9999px;
      background-color: rgba(16, 185, 129, 0.15);
      color: #34d399;
      border: 1px solid rgba(16, 185, 129, 0.3);
      margin-top: 6px;
    }
    .card {
      background-color: #18181b;
      border: 1px solid #27272a;
      border-radius: 12px;
      padding: 32px 28px;
      box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.5);
    }
    h1 {
      font-size: 22px;
      font-weight: 600;
      color: #f4f4f5;
      margin-top: 0;
      margin-bottom: 16px;
    }
    p {
      margin: 0 0 16px 0;
      color: #a1a1aa;
      font-size: 14px;
    }
    .claim-box {
      background-color: #09090b;
      border: 1px solid #3f3f46;
      border-radius: 8px;
      padding: 20px;
      text-align: center;
      margin: 24px 0;
    }
    .claim-label {
      font-size: 11px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: #71717a;
      margin-bottom: 6px;
    }
    .claim-code {
      font-family: 'SF Mono', Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
      font-size: 28px;
      font-weight: 700;
      letter-spacing: 0.15em;
      color: #38bdf8;
      margin: 4px 0;
    }
    .claim-expiry {
      font-size: 12px;
      color: #fbbf24;
      margin-top: 6px;
    }
    .command-header {
      font-size: 12px;
      font-weight: 600;
      color: #d4d4d8;
      margin: 20px 0 8px 0;
    }
    .command-box {
      background-color: #09090b;
      border: 1px solid #27272a;
      border-radius: 6px;
      padding: 12px 14px;
      font-family: 'SF Mono', Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
      font-size: 12px;
      color: #10b981;
      word-break: break-all;
      overflow-x: auto;
    }
    .feature-list {
      margin: 24px 0 0 0;
      padding: 0;
      list-style: none;
    }
    .feature-item {
      display: flex;
      font-size: 13px;
      color: #d4d4d8;
      margin-bottom: 8px;
    }
    .feature-icon {
      color: #10b981;
      margin-right: 8px;
      font-weight: bold;
    }
    .divider {
      height: 1px;
      background-color: #27272a;
      margin: 24px 0;
    }
    .token-accordion {
      font-size: 12px;
      color: #71717a;
      word-break: break-all;
      font-family: monospace;
      background-color: #09090b;
      padding: 10px;
      border-radius: 4px;
      border: 1px solid #27272a;
      max-height: 80px;
      overflow-y: auto;
    }
    .footer {
      text-align: center;
      padding-top: 24px;
      font-size: 12px;
      color: #52525b;
    }
    .footer a {
      color: #71717a;
      text-decoration: underline;
    }
  </style>
</head>
<body>
  <div class="wrapper">
    <div class="brand-header">
      <div class="brand-logo">MANOVA</div>
      <div class="brand-badge">Orbit Developer Platform</div>
    </div>

    <div class="card">
      <h1>Welcome to the team, {{if .RecipientName}}{{.RecipientName}}{{else}}Developer{{end}}</h1>
      <p>Your Manova developer workspace and subsystem credentials are provisioned and ready for activation.</p>

      <div class="claim-box">
        <div class="claim-label">Your Onboarding Activation Token</div>
        <div class="claim-code">{{.Token}}</div>
        <div class="claim-expiry">⚠ Valid for {{.ExpiresInHuman}} (expires {{.ExpiresAt.Format "2006-01-02 15:04 UTC"}})</div>
      </div>

      <div class="command-header">1-Step Activation Terminal Command:</div>
      <div class="command-box">{{.CLICommand}}</div>

      <div class="command-header" style="margin-top: 14px;">Or setup on a fresh machine (Zero-Install):</div>
      <div class="command-box">{{.CurlCommand}}</div>

      <div class="divider"></div>

      <p style="font-weight: 600; color: #f4f4f5; margin-bottom: 10px;">What will be configured automatically:</p>
      <ul class="feature-list">
        <li class="feature-item"><span class="feature-icon">✔</span> Git (Forgejo) account & SSH authentication</li>
        <li class="feature-item"><span class="feature-icon">✔</span> WireGuard VPN dev tunnel credentials</li>
        <li class="feature-item"><span class="feature-icon">✔</span> LLDAP developer SSO directory</li>
        <li class="feature-item"><span class="feature-icon">✔</span> Cursor IDE agent rules & MCP environment</li>
      </ul>
    </div>

    <div class="footer">
      <p>Need help? Check the <a href="https://handbook.dev.manova.space">Manova Handbook</a> or contact platform ops.</p>
      <p>&copy; 2026 Manova Platform Engineering. All rights reserved.</p>
    </div>
  </div>
</body>
</html>`

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
	name := strings.TrimSpace(data.RecipientName)
	if name == "" {
		name = "Developer"
	}

	code := data.ShortCode
	if code == "" {
		code = data.Token
	}
	subject = fmt.Sprintf("Welcome to Manova — Developer Workspace Invitation (%s)", code)

	// Defaults for commands if not provided
	if data.CLICommand == "" {
		data.CLICommand = fmt.Sprintf("orbit onboard --token %s", data.Token)
	}
	if data.CurlCommand == "" {
		data.CurlCommand = fmt.Sprintf("curl -fsSL https://get.manova.space | bash -s -- onboard --token %s", data.Token)
	}
	if data.ExpiresInHuman == "" && !data.ExpiresAt.IsZero() {
		data.ExpiresInHuman = FormatRemaining(time.Until(data.ExpiresAt))
	}

	// Render PlainText
	tt, err := texttmpl.New("invite_text").Parse(plainTextTemplate)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse text email template: %w", err)
	}
	var textBuf bytes.Buffer
	if err := tt.Execute(&textBuf, data); err != nil {
		return "", "", "", fmt.Errorf("failed to execute text email template: %w", err)
	}
	textBody = textBuf.String()

	// Render HTML
	ht, err := htmltmpl.New("invite_html").Parse(htmlEmailTemplate)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse html email template: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := ht.Execute(&htmlBuf, data); err != nil {
		return "", "", "", fmt.Errorf("failed to execute html email template: %w", err)
	}
	htmlBody = htmlBuf.String()

	return subject, textBody, htmlBody, nil
}

// OwnerChallengeEmailData holds template variables for owner verification OTP challenge emails.
type OwnerChallengeEmailData struct {
	OwnerEmail  string
	OTPCode     string
	ExpiresIn   string
	ServerHost  string
	GeneratedAt time.Time
}

const plainTextOwnerChallengeTemplate = `================================================================================
ORBIT PLATFORM — SERVER OWNERSHIP VERIFICATION
================================================================================

A server ownership verification request was initiated for the Orbit Platform.

VERIFICATION ONE-TIME CODE:
--------------------------------------------------------------------------------
{{.OTPCode}}
--------------------------------------------------------------------------------

Target Email: {{.OwnerEmail}}
Expires In:   {{if .ExpiresIn}}{{.ExpiresIn}}{{else}}10 minutes{{end}}
{{if .ServerHost}}Server Host:  {{.ServerHost}}
{{end}}Generated At: {{if not .GeneratedAt.IsZero}}{{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}{{else}}Just now{{end}}

--------------------------------------------------------------------------------
SECURITY NOTICE
--------------------------------------------------------------------------------
This one-time verification code confirms administrative authority and ownership
over the Orbit platform deployment.

If you did not initiate this verification, no action is required. If you suspect
unauthorized activity, please notify security@manova.space immediately.

Never share this code with anyone.

--
Orbit Platform Security & Operations
Manova Engineering Workspace
`

const htmlOwnerChallengeTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Orbit Platform — Server Ownership Verification Code</title>
  <style>
    body {
      margin: 0;
      padding: 0;
      background-color: #09090b;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
      color: #e4e4e7;
      line-height: 1.6;
    }
    .wrapper {
      max-width: 600px;
      margin: 0 auto;
      padding: 32px 20px;
    }
    .brand-header {
      text-align: center;
      padding-bottom: 24px;
    }
    .brand-logo {
      font-size: 20px;
      font-weight: 700;
      letter-spacing: 0.05em;
      color: #ffffff;
      text-transform: uppercase;
    }
    .brand-badge {
      display: inline-block;
      font-size: 11px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      padding: 3px 10px;
      border-radius: 9999px;
      background-color: rgba(56, 189, 248, 0.15);
      color: #38bdf8;
      border: 1px solid rgba(56, 189, 248, 0.3);
      margin-top: 6px;
    }
    .card {
      background-color: #18181b;
      border: 1px solid #27272a;
      border-radius: 12px;
      padding: 32px 28px;
      box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.5);
    }
    h1 {
      font-size: 20px;
      font-weight: 600;
      color: #f4f4f5;
      margin-top: 0;
      margin-bottom: 16px;
    }
    p {
      margin: 0 0 16px 0;
      color: #a1a1aa;
      font-size: 14px;
    }
    .code-box {
      background-color: #09090b;
      border: 1px solid #38bdf8;
      border-radius: 8px;
      padding: 24px 20px;
      text-align: center;
      margin: 24px 0;
    }
    .code-label {
      font-size: 11px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: #a1a1aa;
      margin-bottom: 8px;
    }
    .code-value {
      font-family: 'SF Mono', Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
      font-size: 34px;
      font-weight: 700;
      letter-spacing: 0.25em;
      color: #38bdf8;
      margin: 6px 0;
    }
    .code-expiry {
      font-size: 12px;
      color: #fbbf24;
      margin-top: 8px;
    }
    .meta-table {
      width: 100%;
      margin: 20px 0;
      border-collapse: collapse;
    }
    .meta-table td {
      padding: 6px 0;
      font-size: 13px;
      border-bottom: 1px solid #27272a;
    }
    .meta-label {
      color: #71717a;
      width: 35%;
    }
    .meta-value {
      color: #d4d4d8;
      font-family: 'SF Mono', Monaco, Consolas, monospace;
      font-size: 12px;
    }
    .security-box {
      background-color: rgba(16, 185, 129, 0.08);
      border: 1px solid rgba(16, 185, 129, 0.25);
      border-radius: 6px;
      padding: 14px;
      margin-top: 20px;
    }
    .security-title {
      font-size: 12px;
      font-weight: 600;
      color: #10b981;
      margin-bottom: 4px;
    }
    .security-text {
      font-size: 12px;
      color: #a1a1aa;
      margin: 0;
      line-height: 1.5;
    }
    .divider {
      height: 1px;
      background-color: #27272a;
      margin: 24px 0;
    }
    .footer {
      text-align: center;
      padding-top: 24px;
      font-size: 12px;
      color: #52525b;
    }
    .footer a {
      color: #71717a;
      text-decoration: underline;
    }
  </style>
</head>
<body>
  <div class="wrapper">
    <div class="brand-header">
      <div class="brand-logo">MANOVA</div>
      <div class="brand-badge">Orbit Platform Ownership</div>
    </div>

    <div class="card">
      <h1>Server Ownership Verification</h1>
      <p>A request has been made to verify server and platform administrative ownership for <strong>{{.OwnerEmail}}</strong>.</p>

      <div class="code-box">
        <div class="code-label">Verification One-Time Code</div>
        <div class="code-value">{{.OTPCode}}</div>
        <div class="code-expiry">⚠ Valid for {{if .ExpiresIn}}{{.ExpiresIn}}{{else}}10 minutes{{end}}</div>
      </div>

      <table class="meta-table">
        <tr>
          <td class="meta-label">Target Email</td>
          <td class="meta-value">{{.OwnerEmail}}</td>
        </tr>
        {{if .ServerHost}}
        <tr>
          <td class="meta-label">Server Host</td>
          <td class="meta-value">{{.ServerHost}}</td>
        </tr>
        {{end}}
        <tr>
          <td class="meta-label">Generated At</td>
          <td class="meta-value">{{if not .GeneratedAt.IsZero}}{{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}{{else}}Just now{{end}}</td>
        </tr>
      </table>

      <div class="security-box">
        <div class="security-title">🔒 Security Notice</div>
        <p class="security-text">
          This code grants administrative authority over the Orbit platform deployment.
          If you did not request this verification, please ignore this message or contact <a href="mailto:security@manova.space" style="color: #38bdf8;">security@manova.space</a>. Never share this code with anyone.
        </p>
      </div>
    </div>

    <div class="footer">
      <p>Orbit Platform Security &bull; <a href="https://handbook.dev.manova.space">Manova Handbook</a></p>
      <p>&copy; 2026 Manova Platform Engineering. All rights reserved.</p>
    </div>
  </div>
</body>
</html>`

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

	subject = fmt.Sprintf("Orbit Platform — Server Ownership Verification Code (%s)", otp)

	// Render PlainText
	tt, err := texttmpl.New("owner_challenge_text").Parse(plainTextOwnerChallengeTemplate)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse text owner challenge template: %w", err)
	}
	var textBuf bytes.Buffer
	if err := tt.Execute(&textBuf, data); err != nil {
		return "", "", "", fmt.Errorf("failed to execute text owner challenge template: %w", err)
	}
	textBody = textBuf.String()

	// Render HTML
	ht, err := htmltmpl.New("owner_challenge_html").Parse(htmlOwnerChallengeTemplate)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse html owner challenge template: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := ht.Execute(&htmlBuf, data); err != nil {
		return "", "", "", fmt.Errorf("failed to execute html owner challenge template: %w", err)
	}
	htmlBody = htmlBuf.String()

	return subject, textBody, htmlBody, nil
}

