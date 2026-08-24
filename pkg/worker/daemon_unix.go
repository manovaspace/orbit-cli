//go:build !windows

package worker

import (
	"os"
	"os/exec"
	"syscall"
)

func setDetachedProcessAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix systems, FindProcess always succeeds. Sending signal 0 checks if the process actually exists.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Signal(syscall.SIGTERM)
	return nil
}
