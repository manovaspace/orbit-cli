// Package forms provides interactive terminal form helpers for the Orbit CLI.
package forms

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// InviteFormData holds the collected result of an interactive invite form.
type InviteFormData struct {
	Email       string
	Role        string
	TTL         string
	CreateAlias bool
}

// RunInviteForm runs an interactive terminal prompt to collect invite details.
// initialEmail, initialRole, and initialTTL are pre-filled defaults.
func RunInviteForm(initialEmail, initialRole, initialTTL string) (*InviteFormData, error) {
	return RunInviteFormWith(os.Stdin, os.Stdout, initialEmail, initialRole, initialTTL)
}

// RunInviteFormWith runs the interactive form using the given reader/writer (for testing).
func RunInviteFormWith(in io.Reader, out io.Writer, initialEmail, initialRole, initialTTL string) (*InviteFormData, error) {
	reader := bufio.NewReader(in)

	readLine := func(prompt, defaultVal string) (string, error) {
		if defaultVal != "" {
			fmt.Fprintf(out, "%s [%s]: ", prompt, defaultVal)
		} else {
			fmt.Fprintf(out, "%s: ", prompt)
		}
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultVal, nil
		}
		return line, nil
	}

	email, err := readLine("Developer email", initialEmail)
	if err != nil {
		return nil, fmt.Errorf("reading email: %w", err)
	}
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	role, err := readLine("Access scope (core/client/guest)", initialRole)
	if err != nil {
		return nil, fmt.Errorf("reading role: %w", err)
	}
	if role == "" {
		role = "core"
	}

	ttl, err := readLine("Token TTL (e.g. 7d, 24h)", initialTTL)
	if err != nil {
		return nil, fmt.Errorf("reading TTL: %w", err)
	}
	if ttl == "" {
		ttl = "168h"
	}

	aliasStr, err := readLine("Create @manova.space email alias? (Y/n)", "Y")
	if err != nil {
		return nil, fmt.Errorf("reading alias: %w", err)
	}
	createAlias := !strings.EqualFold(strings.TrimSpace(aliasStr), "n") && !strings.EqualFold(strings.TrimSpace(aliasStr), "no")

	return &InviteFormData{
		Email:       email,
		Role:        role,
		TTL:         ttl,
		CreateAlias: createAlias,
	}, nil
}
