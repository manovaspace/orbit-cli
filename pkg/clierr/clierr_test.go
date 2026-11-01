package clierr

import (
	"errors"
	"fmt"
	"testing"
)

func TestConstructors(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		code string
		exit int
	}{
		{"failed", Failed("boom"), "failed", ExitFailed},
		{"usage", Usage("bad flag"), "usage", ExitUsage},
		{"auth", Auth("no token"), "auth", ExitAuth},
		{"network", Network("timeout"), "network", ExitNetwork},
		{"tool", Tool("git missing"), "tool", ExitTool},
		{"unverified", Unverified("cannot check"), "unverified", ExitUnverified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("Code = %q, want %q", tc.err.Code, tc.code)
			}
			if tc.err.Exit != tc.exit {
				t.Errorf("Exit = %d, want %d", tc.err.Exit, tc.exit)
			}
			if got := ExitCode(tc.err); got != tc.exit {
				t.Errorf("ExitCode = %d, want %d", got, tc.exit)
			}
			if got := Code(tc.err); got != tc.code {
				t.Errorf("Code = %q, want %q", got, tc.code)
			}
		})
	}
}

func TestExitCodes(t *testing.T) {
	auth := Auth("denied")
	wrapped := fmt.Errorf("context: %w", auth)

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"plain", errors.New("boom"), ExitFailed},
		{"clierr", auth, ExitAuth},
		{"wrapped", wrapped, ExitAuth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.want {
				t.Errorf("ExitCode = %d, want %d", got, tc.want)
			}
		})
	}

	if got := Code(nil); got != "" {
		t.Errorf("Code(nil) = %q, want empty", got)
	}
	if got := Code(errors.New("boom")); got != CodeFailed {
		t.Errorf("Code(plain) = %q, want %q", got, CodeFailed)
	}
	if got := Code(wrapped); got != CodeAuth {
		t.Errorf("Code(wrapped) = %q, want %q", got, CodeAuth)
	}
}

func TestErrorMessage(t *testing.T) {
	if got := Failed("boom").Error(); got != "boom" {
		t.Errorf("Error() = %q, want %q", got, "boom")
	}

	cause := errors.New("connection refused")
	err := Wrap(cause, Network("cannot reach orbit"))
	if got, want := err.Error(), "cannot reach orbit: connection refused"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestUnwrap(t *testing.T) {
	cause := errors.New("exit status 1")
	err := Wrap(cause, Tool("git failed"))
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is did not find the wrapped cause")
	}
	if got := err.Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
	if got := Failed("boom").Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

func TestWithHint(t *testing.T) {
	err := WithHint(Failed("boom"), "run orbit doctor")
	if err.Hint != "run orbit doctor" {
		t.Errorf("Hint = %q, want %q", err.Hint, "run orbit doctor")
	}
	if err.Exit != ExitFailed {
		t.Errorf("Exit = %d, want %d", err.Exit, ExitFailed)
	}
}
