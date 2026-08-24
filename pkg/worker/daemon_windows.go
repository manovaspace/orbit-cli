//go:build windows

package worker

import (
	"os"
	"os/exec"
)

func setDetachedProcessAttr(cmd *exec.Cmd) {
	// Windows-specific detached process attributes
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc != nil
}

func killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
