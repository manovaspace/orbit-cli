package domain

import (
	"fmt"
	"strings"
)

// SanitizeSession returns a copy of s with every secret masked so it can be
// shipped in a diagnostic bundle.
func SanitizeSession(s *SessionState) *SessionState {
	if s == nil {
		return nil
	}
	sanitized := *s
	if sanitized.InviteToken != "" {
		if len(sanitized.InviteToken) > 16 {
			sanitized.InviteToken = sanitized.InviteToken[:10] + "..." + sanitized.InviteToken[len(sanitized.InviteToken)-4:]
		} else {
			sanitized.InviteToken = "orbit-inv.***"
		}
	}
	if sanitized.ForgejoToken != "" {
		sanitized.ForgejoToken = "fjo_tok_***"
	}
	if sanitized.WireGuardConfig != "" {
		lines := strings.Split(sanitized.WireGuardConfig, "\n")
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "PrivateKey") {
				lines[i] = "PrivateKey = [REDACTED]"
			}
		}
		sanitized.WireGuardConfig = strings.Join(lines, "\n")
	}
	return &sanitized
}

// SanitizeEnvContent masks the values of sensitive environment variables in an
// env-file body, keeping everything else readable.
func SanitizeEnvContent(content string) string {
	lines := strings.Split(content, "\n")
	sensitiveKeys := []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "PRIVATE", "AUTH"}
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			result = append(result, line)
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.ToUpper(strings.TrimSpace(parts[0]))
			isSensitive := false
			for _, sk := range sensitiveKeys {
				if strings.Contains(key, sk) {
					isSensitive = true
					break
				}
			}
			if isSensitive {
				result = append(result, fmt.Sprintf("%s=[REDACTED]", parts[0]))
			} else {
				result = append(result, line)
			}
		} else {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}
