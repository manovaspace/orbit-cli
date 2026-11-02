// Package machine implements the diagnostics host port with the os package.
package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/host"
)

// Host is the real local machine.
type Host struct{}

// New builds a Host.
func New() *Host { return &Host{} }

// Report evaluates the live host against Orbit constraints.
func (h *Host) Report() host.Report { return host.Live() }

// Resources reads free space on "/" and on the volume holding the workspace,
// plus MemAvailable. Unreadable values stay zero so nothing is claimed.
func (h *Host) Resources() domain.Resources {
	var resources domain.Resources
	paths := []string{"/"}
	if cwd, err := os.Getwd(); err == nil && cwd != "/" {
		paths = append(paths, cwd)
	}
	seen := map[uint64]bool{}
	for _, path := range paths {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(path, &stat); err != nil {
			continue
		}
		free := uint64(stat.Bavail) * uint64(stat.Bsize)
		// The two paths may share one filesystem; report it once.
		if free == 0 || seen[free] {
			continue
		}
		seen[free] = true
		resources.Disks = append(resources.Disks, domain.Volume{Path: path, FreeBytes: free})
	}
	resources.MemAvailableBytes = memAvailable("/proc/meminfo")
	return resources
}

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

// memAvailable reads the MemAvailable line of /proc/meminfo in bytes.
func memAvailable(path string) uint64 {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		if fields[1] == "" {
			continue
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kilobytes * 1024
	}
	return 0
}
