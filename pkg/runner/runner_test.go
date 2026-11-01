package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

func TestExecSuccess(t *testing.T) {
	res, err := Exec{}.Run(context.Background(), "", "sh", "-c", "printf hello; printf err >&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "hello" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "hello")
	}
	if res.Stderr != "err" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "err")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
}

func TestExecDir(t *testing.T) {
	dir := t.TempDir()
	res, err := Exec{}.Run(context.Background(), dir, "pwd")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != dir+"\n" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, dir+"\n")
	}
}

func TestExecNonZeroExit(t *testing.T) {
	res, err := Exec{}.Run(context.Background(), "", "sh", "-c", "printf out; printf err >&2; exit 7")
	if err == nil {
		t.Fatal("Run returned nil error for a nonzero exit")
	}
	if res.Stdout != "out" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "out")
	}
	if res.Stderr != "err" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "err")
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if got := clierr.Code(err); got != "tool" {
		t.Errorf("Code = %q, want %q", got, "tool")
	}
	if got := clierr.ExitCode(err); got != clierr.ExitTool {
		t.Errorf("ExitCode = %d, want %d", got, clierr.ExitTool)
	}
}

func TestExecMissingBinary(t *testing.T) {
	_, err := Exec{}.Run(context.Background(), "", "orbit-command-that-does-not-exist")
	if err == nil {
		t.Fatal("Run returned nil error for a missing binary")
	}
	if got := clierr.Code(err); got != "tool" {
		t.Errorf("Code = %q, want %q", got, "tool")
	}
	if got := clierr.ExitCode(err); got != clierr.ExitTool {
		t.Errorf("ExitCode = %d, want %d", got, clierr.ExitTool)
	}
}

func TestExecCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Exec{}.Run(ctx, "", "true")
	if err == nil {
		t.Fatal("Run returned nil error for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
}
