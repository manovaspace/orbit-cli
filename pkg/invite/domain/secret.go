package domain

// DefaultSecretEnv is the environment variable that carries the signing secret.
const DefaultSecretEnv = "ORBIT_INVITE_SECRET"

// DefaultFallbackSecret is the development signing secret used when nothing
// else is configured. It is intentionally public.
const DefaultFallbackSecret = "orbit-dev-insecure-invitation-signing-secret-key-32bytes"

// ResolveSecret picks the signing secret in documented precedence order and
// reports whether the insecure development fallback was used. lookup supplies
// the environment so the caller owns process state.
func ResolveSecret(lookup func(string) string, secretFlag, secretEnvFlag, verifiedOwnerSecret string) ([]byte, bool) {
	if secretFlag != "" {
		return []byte(secretFlag), false
	}

	envVar := secretEnvFlag
	if envVar == "" {
		envVar = DefaultSecretEnv
	}

	if val := lookup(envVar); val != "" {
		return []byte(val), false
	}

	if val := lookup("MANOVA_INVITE_SECRET"); val != "" {
		return []byte(val), false
	}

	if jwtVal := lookup("ORBIT_JWT_SECRET"); jwtVal != "" {
		return []byte(jwtVal), false
	}

	if jwtVal := lookup("MANOVA_JWT_SECRET"); jwtVal != "" {
		return []byte(jwtVal), false
	}

	if verifiedOwnerSecret != "" {
		return []byte(verifiedOwnerSecret), false
	}

	return []byte(DefaultFallbackSecret), true
}

// ResolveStaffSecret follows the documented staff invite order: verified owner
// secret first, then the invite environment variables, then the dev fallback.
func ResolveStaffSecret(lookup func(string) string, verifiedOwnerSecret string) []byte {
	if verifiedOwnerSecret != "" {
		return []byte(verifiedOwnerSecret)
	}
	if env := lookup("ORBIT_INVITE_SECRET"); env != "" {
		return []byte(env)
	}
	if env := lookup("MANOVA_INVITE_SECRET"); env != "" {
		return []byte(env)
	}
	return []byte(DefaultFallbackSecret)
}
