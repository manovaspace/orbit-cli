// Package machine implements the diagnostics host port with the os package.
package machine

import (
	"os"
	"os/exec"

	"github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/host"
)

// Host is the real local machine.
type Host struct{}

// New builds a Host.
func New() *Host { return &Host{} }

// Report evaluates the live host against Orbit constraints.
func (h *Host) Report() host.Report { return host.Live() }

// LookPath resolves an external program on PATH.
func (h *Host) LookPath(name string) (string, error) { return exec.LookPath(name) }

// Getwd returns the current working directory.
func (h *Host) Getwd() (string, error) { return os.Getwd() }

// ReadFile reads path.
func (h *Host) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// Stat reports file metadata.
func (h *Host) Stat(path string) (application.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return application.FileInfo{}, err
	}
	return application.FileInfo{IsDir: info.IsDir()}, nil
}

// Lstat reports file metadata without following a final symlink.
func (h *Host) Lstat(path string) (application.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return application.FileInfo{}, err
	}
	return application.FileInfo{IsDir: info.IsDir()}, nil
}

// Getenv reads one environment variable.
func (h *Host) Getenv(name string) string { return os.Getenv(name) }
