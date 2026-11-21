package attached

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

func TestRunAttachesStreams(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	var out, errOut bytes.Buffer
	r := New(strings.NewReader(""), &out, &errOut)
	if err := r.Run(context.Background(), []string{"sh", "-c", "echo hello; echo oops >&2"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "hello\n" || errOut.String() != "oops\n" {
		t.Errorf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestRunNonzeroExitIsToolError(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	r := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	err := r.Run(context.Background(), []string{"sh", "-c", "exit 3"})
	if clierr.ExitCode(err) != clierr.ExitTool || !strings.Contains(err.Error(), "exited with code 3") {
		t.Errorf("err = %v (exit %d), want tool error with code 3", err, clierr.ExitCode(err))
	}
}

func TestRunMissingBinaryIsToolError(t *testing.T) {
	r := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	err := r.Run(context.Background(), []string{"orbit-no-such-binary-xyz"})
	if clierr.ExitCode(err) != clierr.ExitTool {
		t.Errorf("exit = %d, want %d (%v)", clierr.ExitCode(err), clierr.ExitTool, err)
	}
}

func TestRunEmptyArgvIsUsageError(t *testing.T) {
	r := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err := r.Run(context.Background(), nil); clierr.ExitCode(err) != clierr.ExitUsage {
		t.Errorf("exit = %d, want usage", clierr.ExitCode(err))
	}
}
