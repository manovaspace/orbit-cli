package forms_test

import (
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/tui/forms"
)

func TestRunInviteFormWith_Defaults(t *testing.T) {
	input := "\n\n\n\n" // accept all defaults
	in := strings.NewReader(input)
	out := &strings.Builder{}

	data, err := forms.RunInviteFormWith(in, out, "dev@example.com", "core", "7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Email != "dev@example.com" {
		t.Errorf("expected email dev@example.com, got %q", data.Email)
	}
	if data.Role != "core" {
		t.Errorf("expected role core, got %q", data.Role)
	}
	if data.TTL != "7d" {
		t.Errorf("expected TTL 7d, got %q", data.TTL)
	}
	if !data.CreateAlias {
		t.Error("expected CreateAlias true by default")
	}
}

func TestRunInviteFormWith_DeclineAlias(t *testing.T) {
	input := "\n\n\nn\n" // decline alias with 'n'
	in := strings.NewReader(input)
	out := &strings.Builder{}

	data, err := forms.RunInviteFormWith(in, out, "dev@example.com", "core", "7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.CreateAlias {
		t.Error("expected CreateAlias false when explicit 'n' is passed")
	}
}

func TestRunInviteFormWith_CustomValues(t *testing.T) {
	input := "guest@company.com\nclient\n24h\ny\n"
	in := strings.NewReader(input)
	out := &strings.Builder{}

	data, err := forms.RunInviteFormWith(in, out, "", "core", "168h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Email != "guest@company.com" {
		t.Errorf("expected email guest@company.com, got %q", data.Email)
	}
	if data.Role != "client" {
		t.Errorf("expected role client, got %q", data.Role)
	}
	if data.TTL != "24h" {
		t.Errorf("expected TTL 24h, got %q", data.TTL)
	}
	if !data.CreateAlias {
		t.Error("expected CreateAlias true")
	}
}

func TestRunInviteFormWith_MissingEmail(t *testing.T) {
	input := "\n\n\n\n"
	in := strings.NewReader(input)
	out := &strings.Builder{}

	_, err := forms.RunInviteFormWith(in, out, "", "", "")
	if err == nil {
		t.Error("expected error for missing email, got nil")
	}
}
